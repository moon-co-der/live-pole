package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/models"
	"live-polling/internal/redisx"
	"live-polling/internal/repository"
)

const voterLockTTL = 365 * 24 * time.Hour

var (
	ErrPublicPollNotFound = errors.New("public poll not found")
	ErrInvalidVote        = errors.New("invalid vote")
	ErrClosedPoll         = errors.New("poll is closed")
	ErrAlreadyVoted       = errors.New("already voted")
)

type PublicPollCounts interface {
	Exists(context.Context, string) (bool, error)
	HGetAll(context.Context, string) (map[string]int64, error)
	HSetTTL(context.Context, string, map[string]string, time.Duration) error
	SetNX(context.Context, string, time.Duration) (bool, error)
	Set(context.Context, string, string, time.Duration) error
	CastVoteWithTTL(context.Context, string, string, string, []string, time.Duration, redisx.VoteStreamEntry) (redisx.VoteResult, bool, error)
}

type PollPublisher interface {
	Publish(context.Context, string, string) error
}

type PublicPollService struct {
	polls         repository.PollStore
	counts        PublicPollCounts
	votes         repository.VoteStore
	views         repository.ViewStore
	publisher     PollPublisher
	analyticsSalt string
}

type PublicPollResultOption struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Votes int64  `json:"votes"`
}

type PublicPollResults struct {
	Options    []PublicPollResultOption `json:"options"`
	TotalVotes int64                    `json:"totalVotes"`
}

type PublicPollResponse struct {
	Poll     models.PublicPoll  `json:"poll"`
	HasVoted bool               `json:"hasVoted"`
	Results  *PublicPollResults `json:"results,omitempty"`
}

func NewPublicPollService(polls repository.PollStore, counts PublicPollCounts, votes repository.VoteStore, views repository.ViewStore, publisher PollPublisher, salt ...string) *PublicPollService {
	analyticsSalt := "livepoll-analytics-local"
	if len(salt) > 0 && len(salt[0]) >= 32 {
		analyticsSalt = salt[0]
	}
	return &PublicPollService{polls: polls, counts: counts, votes: votes, views: views, publisher: publisher, analyticsSalt: analyticsSalt}
}

func (s *PublicPollService) Get(ctx context.Context, slug, voterID, referer string) (*PublicPollResponse, error) {
	return s.GetTracked(ctx, slug, voterID, referer, "", "")
}

func (s *PublicPollService) GetTracked(ctx context.Context, slug, voterID, referer, userAgent, country string) (*PublicPollResponse, error) {
	poll, err := s.findBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if poll == nil || poll.Status == models.PollDraft {
		return nil, ErrPublicPollNotFound
	}
	if err := EnsurePollCounts(ctx, poll, s.counts, s.votes); err != nil {
		return nil, err
	}

	voterHash := hashVoterID(voterID)
	viewKey := "viewed:" + poll.ID.Hex() + ":" + voterHash
	newView, err := s.counts.SetNX(ctx, viewKey, voterLockTTL)
	if err != nil {
		return nil, err
	}
	if newView {
		if s.views != nil {
			if err := s.views.Create(ctx, &models.View{PollID: poll.ID, CreatedAt: time.Now().UTC(), Source: sourceFromReferer(referer), Device: deviceFromUserAgent(userAgent), Country: country, VisitorHash: s.analyticsVisitorHash(voterID)}); err != nil {
				log.Printf("view event insert failed for poll %s: %v", poll.ID.Hex(), err)
			} else if s.publisher != nil {
				message, _ := json.Marshal(map[string]any{"type": "view_update", "ownerId": poll.Owner.Hex(), "pollId": poll.ID.Hex()})
				if err := s.publisher.Publish(ctx, "poll:"+poll.ID.Hex(), string(message)); err != nil {
					log.Printf("live analytics view publish failed for poll %s: %v", poll.ID.Hex(), err)
				}
			}
		}
		viewCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		if err := retry(2, func() error { return s.polls.IncrementViews(viewCtx, poll.ID) }); err != nil {
			log.Printf("public poll view count update failed for poll %s: %v", poll.ID.Hex(), err)
		}
		cancel()
	}

	voterKey := "voted:" + poll.ID.Hex() + ":" + voterHash
	hasVoted, err := s.counts.Exists(ctx, voterKey)
	if err != nil {
		return nil, err
	}
	public := toPublicPoll(poll)
	if poll.Status == models.PollActive && poll.EndsAt != nil && !poll.EndsAt.After(time.Now()) {
		public.Status = models.PollCompleted
	}
	response := &PublicPollResponse{Poll: public, HasVoted: hasVoted}
	if hasVoted {
		counts, err := s.counts.HGetAll(ctx, "poll:"+poll.ID.Hex()+":counts")
		if err != nil {
			return nil, err
		}
		response.Results = buildPublicResults(poll, counts)
	}
	return response, nil
}

func (s *PublicPollService) Vote(ctx context.Context, slug, voterID, optionID, userAgent, referer string) (*PublicPollResults, error) {
	return s.VoteTracked(ctx, slug, voterID, optionID, userAgent, referer, "")
}

func (s *PublicPollService) VoteTracked(ctx context.Context, slug, voterID, optionID, userAgent, referer, country string) (*PublicPollResults, error) {
	if strings.TrimSpace(optionID) == "" || utf8.RuneCountInString(optionID) > 64 {
		return nil, ErrInvalidVote
	}
	poll, err := s.findBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if poll == nil || poll.Status == models.PollDraft {
		return nil, ErrPublicPollNotFound
	}
	if poll.Status != models.PollActive || (poll.EndsAt != nil && !poll.EndsAt.After(time.Now())) {
		return nil, ErrClosedPoll
	}
	if err := EnsurePollCounts(ctx, poll, s.counts, s.votes); err != nil {
		return nil, err
	}
	optionIDs := make([]string, 0, len(poll.Options))
	validOption := false
	for _, option := range poll.Options {
		optionIDs = append(optionIDs, option.ID)
		if option.ID == optionID {
			validOption = true
		}
	}
	if !validOption {
		return nil, ErrInvalidVote
	}

	createdAt := time.Now().UTC()
	voteID := bson.NewObjectID().Hex()
	device := deviceFromUserAgent(userAgent)
	voterHash := hashVoterID(voterID)
	result, accepted, err := s.counts.CastVoteWithTTL(ctx,
		"voted:"+poll.ID.Hex()+":"+voterHash,
		"poll:"+poll.ID.Hex()+":counts",
		optionID,
		optionIDs,
		pollCountsTTL(poll),
		redisx.VoteStreamEntry{VoteID: voteID, PollID: poll.ID.Hex(), OptionID: optionID, CreatedAt: createdAt.Format(time.RFC3339Nano), Device: device, VoterHash: voterHash, Source: sourceFromReferer(referer), Country: country, VisitorHash: s.analyticsVisitorHash(voterID)},
	)
	if err != nil {
		return nil, err
	}
	if !accepted {
		return nil, ErrAlreadyVoted
	}

	if s.publisher != nil {
		message, _ := json.Marshal(map[string]any{"type": "update", "counts": result.Counts, "total": result.Total, "ownerId": poll.Owner.Hex()})
		if err := s.publisher.Publish(ctx, "poll:"+poll.ID.Hex(), string(message)); err != nil {
			log.Printf("live poll publish failed for poll %s: %v", poll.ID.Hex(), err)
		}
	}
	return buildPublicResultsWithCounts(poll, result.Counts, result.Total), nil
}

// CreateLiveTicket issues a short-lived, single-use credential only for a voter
// whose vote marker is already persisted in Redis.
func (s *PublicPollService) CreateLiveTicket(ctx context.Context, slug, voterID, ticket string) error {
	poll, err := s.findBySlug(ctx, slug)
	if err != nil {
		return err
	}
	if poll == nil || poll.Status == models.PollDraft {
		return ErrPublicPollNotFound
	}
	voterHash := hashVoterID(voterID)
	return s.counts.Set(ctx, "poll-live-ticket:"+ticket, poll.ID.Hex()+":"+voterHash, 30*time.Second)
}

func (s *PublicPollService) Results(ctx context.Context, slug string) (*PublicPollResults, error) {
	poll, err := s.findBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if poll == nil || poll.Status == models.PollDraft {
		return nil, ErrPublicPollNotFound
	}
	if err := EnsurePollCounts(ctx, poll, s.counts, s.votes); err != nil {
		return nil, err
	}
	counts, err := s.counts.HGetAll(ctx, "poll:"+poll.ID.Hex()+":counts")
	if err != nil {
		return nil, err
	}
	return buildPublicResults(poll, counts), nil
}

func (s *PublicPollService) findBySlug(ctx context.Context, slug string) (*models.Poll, error) {
	if len(slug) != 8 {
		return nil, ErrPublicPollNotFound
	}
	for _, char := range slug {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') {
			return nil, ErrPublicPollNotFound
		}
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	poll, err := s.polls.FindBySlug(lookupCtx, slug)
	cancel()
	cache, cacheAvailable := s.publisher.(PollMetadataCache)
	if err == nil {
		if poll != nil && cacheAvailable {
			if cacheErr := cachePollMetadata(ctx, cache, poll); cacheErr != nil {
				log.Printf("poll metadata cache refresh failed for poll %s: %v", poll.ID.Hex(), cacheErr)
			}
		}
		return poll, nil
	}
	if !cacheAvailable {
		return nil, err
	}
	cached, found, cacheErr := readCachedPoll(ctx, cache, slug)
	if cacheErr == nil && found {
		return cached, nil
	}
	return nil, err
}

func sourceFromReferer(referer string) string {
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Hostname() == "" {
		return "direct"
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.Contains(host, "whatsapp") || strings.Contains(host, "wa.me"):
		return "whatsapp"
	case strings.Contains(host, "instagram"):
		return "instagram"
	case strings.Contains(host, "twitter") || strings.Contains(host, "x.com") || strings.Contains(host, "t.co"):
		return "twitter/x"
	default:
		return "other"
	}
}

func toPublicPoll(poll *models.Poll) models.PublicPoll {
	options := make([]models.PublicPollOption, 0, len(poll.Options))
	for _, option := range poll.Options {
		options = append(options, models.PublicPollOption{ID: option.ID, Text: option.Text})
	}
	return models.PublicPoll{Question: poll.Question, Options: options, Status: poll.Status}
}

func buildPublicResults(poll *models.Poll, counts map[string]int64) *PublicPollResults {
	total := counts["total"]
	if total == 0 {
		total = poll.TotalVotes
	}
	return buildPublicResultsWithCounts(poll, counts, total)
}

func buildPublicResultsWithCounts(poll *models.Poll, counts map[string]int64, total int64) *PublicPollResults {
	options := make([]PublicPollResultOption, 0, len(poll.Options))
	for _, option := range poll.Options {
		options = append(options, PublicPollResultOption{ID: option.ID, Text: option.Text, Votes: counts[option.ID]})
	}
	return &PublicPollResults{Options: options, TotalVotes: total}
}

func hashVoterID(voterID string) string {
	sum := sha256.Sum256([]byte(voterID))
	return hex.EncodeToString(sum[:])
}

func (s *PublicPollService) analyticsVisitorHash(visitorID string) string {
	mac := hmac.New(sha256.New, []byte(s.analyticsSalt))
	_, _ = mac.Write([]byte(visitorID))
	return hex.EncodeToString(mac.Sum(nil))
}

func deviceFromUserAgent(userAgent string) string {
	ua := strings.ToLower(userAgent)
	if strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet") || strings.Contains(ua, "kindle") {
		return "tablet"
	}
	if strings.Contains(ua, "mobile") || strings.Contains(ua, "android") || strings.Contains(ua, "iphone") {
		return "mobile"
	}
	return "desktop"
}

func retry(attempts int, operation func() error) error {
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if err = operation(); err == nil {
			return nil
		}
		if attempt+1 < attempts {
			time.Sleep(time.Duration(attempt+1) * 40 * time.Millisecond)
		}
	}
	return err
}
