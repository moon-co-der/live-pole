package services

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/models"
	"live-polling/internal/repository"
)

var (
	ErrInvalidPoll  = errors.New("invalid poll")
	ErrPollNotFound = errors.New("poll not found")
)

type PollCounts interface {
	HSetTTL(context.Context, string, map[string]string, time.Duration) error
	PollMetadataCache
}

type pollRealtimeCounts interface {
	HGetAll(context.Context, string) (map[string]int64, error)
	Publish(context.Context, string, string) error
}

type pollMaintenanceCounts interface {
	pollRealtimeCounts
	SetNX(context.Context, string, time.Duration) (bool, error)
	DeletePollKeys(context.Context, string, string) error
}

type PollService struct {
	polls    repository.PollStore
	votes    repository.VoteStore
	views    repository.ViewStore
	counts   PollCounts
	redisURL string
}

type CreatePollInput struct {
	Question      string     `json:"question"`
	Options       []string   `json:"options"`
	AllowMultiple bool       `json:"allowMultiple"`
	EndsAt        *time.Time `json:"endsAt"`
}

type PollListQuery struct {
	Status string
	Search string
	Sort   string
	Page   int64
	Limit  int64
}

func NewPollService(polls repository.PollStore, votes repository.VoteStore, views repository.ViewStore, counts PollCounts, redisURL string) *PollService {
	return &PollService{polls: polls, votes: votes, views: views, counts: counts, redisURL: redisURL}
}

func (s *PollService) Create(ctx context.Context, ownerID string, input CreatePollInput) (*models.Poll, string, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, "", ErrInvalidPoll
	}
	question := strings.TrimSpace(input.Question)
	if length := utf8.RuneCountInString(question); length < 3 || length > 200 {
		return nil, "", ErrInvalidPoll
	}
	if len(input.Options) < 2 || len(input.Options) > 6 {
		return nil, "", ErrInvalidPoll
	}
	options := make([]models.PollOption, 0, len(input.Options))
	seen := make([]string, 0, len(input.Options))
	for i, raw := range input.Options {
		text := strings.TrimSpace(raw)
		length := utf8.RuneCountInString(text)
		if length == 0 || length > 100 {
			return nil, "", ErrInvalidPoll
		}
		for _, existing := range seen {
			if strings.EqualFold(existing, text) {
				return nil, "", ErrInvalidPoll
			}
		}
		seen = append(seen, text)
		options = append(options, models.PollOption{ID: fmt.Sprintf("option-%d", i+1), Text: text, Votes: 0})
	}
	if input.EndsAt != nil && !input.EndsAt.After(time.Now()) {
		return nil, "", ErrInvalidPoll
	}

	createdAt := time.Now().UTC()
	var poll *models.Poll
	for attempt := 0; attempt < 5; attempt++ {
		slug, err := randomSlug(8)
		if err != nil {
			return nil, "", err
		}
		poll = &models.Poll{
			Owner: owner, Slug: slug, Question: question, Options: options,
			Status: models.PollActive, AllowMultiple: input.AllowMultiple,
			EndsAt: input.EndsAt, TotalVotes: 0, Views: 0,
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}
		if err := s.polls.Create(ctx, poll); err != nil {
			if repository.IsDuplicateSlug(err) {
				continue
			}
			return nil, "", err
		}
		break
	}
	if poll == nil || poll.ID.IsZero() {
		return nil, "", errors.New("could not allocate a unique poll slug")
	}

	counts := make(map[string]string, len(poll.Options)+1)
	counts["total"] = "0"
	for _, option := range poll.Options {
		counts[option.ID] = "0"
	}
	if err := s.counts.HSetTTL(ctx, "poll:"+poll.ID.Hex()+":counts", counts, pollCountsTTL(poll)); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.polls.DeleteByID(rollbackCtx, poll.ID)
		return nil, "", err
	}
	if err := cachePollMetadata(ctx, s.counts, poll); err != nil {
		log.Printf("poll metadata cache failed for poll %s: %v", poll.ID.Hex(), err)
	}
	return poll, "/p/" + poll.Slug, nil
}

func (s *PollService) List(ctx context.Context, ownerID string, query PollListQuery) ([]models.Poll, int64, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, 0, ErrPollNotFound
	}
	if query.Page < 1 || query.Limit < 1 || query.Limit > 100 {
		return nil, 0, ErrInvalidPoll
	}
	if query.Status != "" && query.Status != string(models.PollDraft) && query.Status != string(models.PollActive) && query.Status != string(models.PollCompleted) {
		return nil, 0, ErrInvalidPoll
	}
	if query.Sort == "" {
		query.Sort = "newest"
	}
	if query.Sort != "newest" && query.Sort != "votes" {
		return nil, 0, ErrInvalidPoll
	}
	if query.Page > 1_000_000 || utf8.RuneCountInString(query.Search) > 200 {
		return nil, 0, ErrInvalidPoll
	}
	filter := repository.PollListFilter{
		Owner: owner, Status: query.Status, Search: strings.TrimSpace(query.Search), Sort: query.Sort,
		Skip: (query.Page - 1) * query.Limit, Limit: query.Limit,
	}
	return s.polls.List(ctx, filter)
}

func (s *PollService) GetOwned(ctx context.Context, ownerID, pollKey string) (*models.Poll, error) {
	owner, ownerErr := bson.ObjectIDFromHex(ownerID)
	if ownerErr != nil {
		return nil, ErrPollNotFound
	}
	var poll *models.Poll
	var err error
	if id, parseErr := bson.ObjectIDFromHex(pollKey); parseErr == nil {
		poll, err = s.polls.FindOwned(ctx, id, owner)
	} else {
		poll, err = s.polls.FindOwnedBySlug(ctx, pollKey, owner)
	}
	if err != nil {
		return nil, err
	}
	if poll == nil {
		return nil, ErrPollNotFound
	}
	return poll, nil
}

func (s *PollService) Close(ctx context.Context, ownerID, pollID string) (*models.Poll, error) {
	poll, err := s.GetOwned(ctx, ownerID, pollID)
	if err != nil {
		return nil, err
	}
	owner, _ := bson.ObjectIDFromHex(ownerID)
	if err := s.polls.CloseOwned(ctx, poll.ID, owner); err != nil {
		return nil, err
	}
	poll.Status = models.PollCompleted
	poll.UpdatedAt = time.Now().UTC()
	if err := cachePollMetadata(ctx, s.counts, poll); err != nil {
		log.Printf("closed poll metadata cache failed for poll %s: %v", poll.ID.Hex(), err)
	}
	countsClient, ok := s.counts.(pollRealtimeCounts)
	if !ok {
		return nil, errors.New("live poll counts are unavailable")
	}
	counts, err := countsClient.HGetAll(ctx, "poll:"+poll.ID.Hex()+":counts")
	if err != nil {
		return nil, err
	}
	total := counts["total"]
	delete(counts, "total")
	message, _ := json.Marshal(map[string]any{"type": "poll_closed", "counts": counts, "total": total})
	if err := countsClient.Publish(ctx, "poll:"+poll.ID.Hex(), string(message)); err != nil {
		return nil, err
	}
	return poll, nil
}

func (s *PollService) Delete(ctx context.Context, ownerID, pollID string) error {
	poll, err := s.GetOwned(ctx, ownerID, pollID)
	if err != nil {
		return err
	}
	countsClient, ok := s.counts.(pollMaintenanceCounts)
	if !ok {
		return errors.New("poll maintenance is unavailable")
	}
	counts, err := countsClient.HGetAll(ctx, "poll:"+poll.ID.Hex()+":counts")
	if err != nil {
		return err
	}
	total := counts["total"]
	delete(counts, "total")
	poll.Status = models.PollCompleted
	if err := cachePollMetadata(ctx, s.counts, poll); err != nil {
		log.Printf("deleting poll cache update failed poll=%s: %v", poll.ID.Hex(), err)
	}
	message, _ := json.Marshal(map[string]any{"type": "poll_closed", "counts": counts, "total": total})
	if err := countsClient.Publish(ctx, "poll:"+poll.ID.Hex(), string(message)); err != nil {
		return err
	}
	if err := s.votes.DeleteByPoll(ctx, poll.ID); err != nil {
		return err
	}
	if err := s.views.DeleteByPoll(ctx, poll.ID); err != nil {
		return err
	}
	if err := s.polls.DeleteOwned(ctx, poll.ID, poll.Owner); err != nil {
		return err
	}
	return countsClient.DeletePollKeys(ctx, poll.ID.Hex(), poll.Slug)
}

func (s *PollService) ExpireDue(ctx context.Context) (int, error) {
	countsClient, ok := s.counts.(pollMaintenanceCounts)
	if !ok {
		return 0, errors.New("poll maintenance is unavailable")
	}
	locked, err := countsClient.SetNX(ctx, "lock:expiry", 9*time.Second)
	if err != nil || !locked {
		return 0, err
	}
	now := time.Now().UTC()
	expired, err := s.polls.FindExpired(ctx, now)
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, poll := range expired {
		changed, err := s.polls.CompleteExpired(ctx, poll.ID, now)
		if err != nil {
			return completed, err
		}
		if !changed {
			continue
		}
		poll.Status = models.PollCompleted
		poll.UpdatedAt = now
		if err := cachePollMetadata(ctx, s.counts, &poll); err != nil {
			log.Printf("expired poll cache update failed poll=%s: %v", poll.ID.Hex(), err)
		}
		counts, err := countsClient.HGetAll(ctx, "poll:"+poll.ID.Hex()+":counts")
		if err != nil {
			return completed, err
		}
		total := counts["total"]
		delete(counts, "total")
		message, _ := json.Marshal(map[string]any{"type": "poll_closed", "counts": counts, "total": total})
		if err := countsClient.Publish(ctx, "poll:"+poll.ID.Hex(), string(message)); err != nil {
			return completed, err
		}
		log.Printf("expired poll %s", poll.ID.Hex())
		completed++
	}
	return completed, nil
}

func randomSlug(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var slug strings.Builder
	slug.Grow(length)
	for range length {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		slug.WriteByte(alphabet[index.Int64()])
	}
	return slug.String(), nil
}
