package services

import (
	"context"
	"sort"
	"time"
	_ "time/tzdata"

	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/models"
	"live-polling/internal/repository"
)

type AnalyticsStore interface {
	OwnerSnapshot(context.Context, bson.ObjectID) (repository.AnalyticsSnapshot, error)
	PollSnapshot(context.Context, bson.ObjectID, string) (*models.Poll, repository.AnalyticsSnapshot, error)
}

type AnalyticsService struct{ repo AnalyticsStore }

func NewAnalyticsService(repo AnalyticsStore) *AnalyticsService {
	return &AnalyticsService{repo: repo}
}

type GrowthStat struct {
	Value  float64 `json:"value"`
	Growth float64 `json:"growth"`
}
type Overview struct {
	TotalPolls        GrowthStat          `json:"totalPolls"`
	TotalVotes        GrowthStat          `json:"totalVotes"`
	TotalViews        GrowthStat          `json:"totalViews"`
	Engagement        GrowthStat          `json:"engagement"`
	PeriodDays        int                 `json:"periodDays"`
	OpenPolls         int64               `json:"openPolls"`
	VotesToday        int64               `json:"votesToday"`
	PollsWithoutVotes int64               `json:"pollsWithoutVotes"`
	Polls             []DashboardPoll     `json:"polls"`
	RecentActivity    []DashboardActivity `json:"recentActivity"`
}

type DashboardPoll struct {
	ID         string            `json:"id"`
	Slug       string            `json:"slug"`
	Question   string            `json:"question"`
	Status     models.PollStatus `json:"status"`
	TotalVotes int64             `json:"totalVotes"`
	CreatedAt  time.Time         `json:"createdAt"`
	LastVoteAt *time.Time        `json:"lastVoteAt,omitempty"`
}

type DashboardActivity struct {
	OptionName string    `json:"optionName"`
	PollTitle  string    `json:"pollTitle"`
	CreatedAt  time.Time `json:"createdAt"`
}
type DailyVotes struct {
	Date  string `json:"date"`
	Votes int64  `json:"votes"`
}
type TopPoll struct {
	ID         string            `json:"id"`
	Slug       string            `json:"slug"`
	Question   string            `json:"question"`
	Status     models.PollStatus `json:"status"`
	TotalVotes int64             `json:"totalVotes"`
	Views      int64             `json:"views"`
}

func (s *AnalyticsService) Overview(ctx context.Context, ownerID string) (Overview, error) {
	return s.OverviewForTimezone(ctx, ownerID, "")
}

func (s *AnalyticsService) OverviewForTimezone(ctx context.Context, ownerID, timezone string) (Overview, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return Overview{}, ErrPollNotFound
	}
	snapshot, err := s.repo.OwnerSnapshot(ctx, owner)
	if err != nil {
		return Overview{}, err
	}
	location := time.Local
	if timezone != "" {
		location, err = time.LoadLocation(timezone)
		if err != nil {
			return Overview{}, ErrInvalidPoll
		}
	}
	now := time.Now().In(location)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).UTC()
	current := now.AddDate(0, 0, -30)
	previous := now.AddDate(0, 0, -60)
	var pollsNow, pollsPrev, votesNow, votesPrev, viewsNow, viewsPrev, openPolls, votesToday, pollsWithoutVotes int64
	pollVotes := make(map[string]int64, len(snapshot.Polls))
	lastVoteAt := make(map[string]time.Time, len(snapshot.Polls))
	pollNames := make(map[string]string, len(snapshot.Polls))
	optionNames := make(map[string]string)
	for _, p := range snapshot.Polls {
		pollNames[p.ID.Hex()] = p.Question
		for _, option := range p.Options {
			optionNames[p.ID.Hex()+":"+option.ID] = option.Text
		}
	}
	for _, p := range snapshot.Polls {
		if p.Status == models.PollActive && (p.EndsAt == nil || p.EndsAt.After(now)) {
			openPolls++
		}
		if !p.CreatedAt.Before(current) {
			pollsNow++
		} else if !p.CreatedAt.Before(previous) {
			pollsPrev++
		}
	}
	for _, v := range snapshot.Votes {
		pollKey := v.PollID.Hex()
		pollVotes[pollKey]++
		if previous, exists := lastVoteAt[pollKey]; !exists || v.CreatedAt.After(previous) {
			lastVoteAt[pollKey] = v.CreatedAt
		}
		if !v.CreatedAt.Before(todayStart) {
			votesToday++
		}
		if !v.CreatedAt.Before(current) {
			votesNow++
		} else if !v.CreatedAt.Before(previous) {
			votesPrev++
		}
	}
	for _, p := range snapshot.Polls {
		if pollVotes[p.ID.Hex()] == 0 {
			pollsWithoutVotes++
		}
	}
	for _, v := range snapshot.Views {
		if !v.CreatedAt.Before(current) {
			viewsNow++
		} else if !v.CreatedAt.Before(previous) {
			viewsPrev++
		}
	}
	allVotes, allViews := int64(len(snapshot.Votes)), int64(len(snapshot.Views))
	engagement := float64(0)
	if allViews > 0 {
		engagement = float64(allVotes) / float64(allViews) * 100
	}
	currentEngagement, previousEngagement := float64(0), float64(0)
	if viewsNow > 0 {
		currentEngagement = float64(votesNow) / float64(viewsNow) * 100
	}
	if viewsPrev > 0 {
		previousEngagement = float64(votesPrev) / float64(viewsPrev) * 100
	}
	polls := append([]models.Poll(nil), snapshot.Polls...)
	sort.Slice(polls, func(i, j int) bool { return polls[i].CreatedAt.After(polls[j].CreatedAt) })
	dashboardPolls := make([]DashboardPoll, 0, min(8, len(polls)))
	for _, p := range polls {
		if len(dashboardPolls) == 8 {
			break
		}
		status := p.Status
		if status == models.PollActive && p.EndsAt != nil && !p.EndsAt.After(now) {
			status = models.PollCompleted
		}
		var last *time.Time
		if value, exists := lastVoteAt[p.ID.Hex()]; exists {
			last = &value
		}
		dashboardPolls = append(dashboardPolls, DashboardPoll{ID: p.ID.Hex(), Slug: p.Slug, Question: p.Question, Status: status, TotalVotes: pollVotes[p.ID.Hex()], CreatedAt: p.CreatedAt, LastVoteAt: last})
	}
	activityVotes := append([]models.Vote(nil), snapshot.Votes...)
	sort.Slice(activityVotes, func(i, j int) bool { return activityVotes[i].CreatedAt.After(activityVotes[j].CreatedAt) })
	if len(activityVotes) > 5 {
		activityVotes = activityVotes[:5]
	}
	activity := make([]DashboardActivity, 0, len(activityVotes))
	for _, vote := range activityVotes {
		pollKey := vote.PollID.Hex()
		activity = append(activity, DashboardActivity{OptionName: optionNames[pollKey+":"+vote.OptionID], PollTitle: pollNames[pollKey], CreatedAt: vote.CreatedAt})
	}
	return Overview{TotalPolls: GrowthStat{float64(len(snapshot.Polls)), growth(pollsNow, pollsPrev)}, TotalVotes: GrowthStat{float64(allVotes), growth(votesNow, votesPrev)}, TotalViews: GrowthStat{float64(allViews), growth(viewsNow, viewsPrev)}, Engagement: GrowthStat{engagement, growthFloat(currentEngagement, previousEngagement)}, PeriodDays: 30, OpenPolls: openPolls, VotesToday: votesToday, PollsWithoutVotes: pollsWithoutVotes, Polls: dashboardPolls, RecentActivity: activity}, nil
}

func (s *AnalyticsService) VotesOverTime(ctx context.Context, ownerID string, days int) ([]DailyVotes, error) {
	if days != 7 && days != 30 {
		return nil, ErrInvalidPoll
	}
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, ErrPollNotFound
	}
	snapshot, err := s.repo.OwnerSnapshot(ctx, owner)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -days+1)
	counts := map[string]int64{}
	for _, vote := range snapshot.Votes {
		if !vote.CreatedAt.Before(start) {
			counts[vote.CreatedAt.UTC().Format("2006-01-02")]++
		}
	}
	series := make([]DailyVotes, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		series = append(series, DailyVotes{key, counts[key]})
	}
	return series, nil
}

func (s *AnalyticsService) TopPolls(ctx context.Context, ownerID string) ([]TopPoll, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, ErrPollNotFound
	}
	snapshot, err := s.repo.OwnerSnapshot(ctx, owner)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, v := range snapshot.Votes {
		counts[v.PollID.Hex()]++
	}
	polls := make([]TopPoll, 0, len(snapshot.Polls))
	for _, p := range snapshot.Polls {
		poll := TopPoll{ID: p.ID.Hex(), Slug: p.Slug, Question: p.Question, Status: p.Status, TotalVotes: counts[p.ID.Hex()], Views: p.Views}
		polls = append(polls, poll)
	}
	sort.Slice(polls, func(i, j int) bool {
		if polls[i].TotalVotes == polls[j].TotalVotes {
			return polls[i].Question < polls[j].Question
		}
		return polls[i].TotalVotes > polls[j].TotalVotes
	})
	if len(polls) > 5 {
		polls = polls[:5]
	}
	return polls, nil
}

type OptionAnalytics struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	Votes      int64   `json:"votes"`
	Percentage float64 `json:"percentage"`
	Winner     bool    `json:"winner"`
}
type PollAnalytics struct {
	Poll           TopPoll           `json:"poll"`
	TotalVotes     int64             `json:"totalVotes"`
	Views          int64             `json:"views"`
	ConversionRate float64           `json:"conversionRate"`
	Options        []OptionAnalytics `json:"options"`
	VotesOverTime  []DailyVotes      `json:"votesOverTime"`
	RecentVotes    []RecentVote      `json:"recentVotes"`
}

type RecentVote struct {
	OptionName string    `json:"optionName"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (s *AnalyticsService) Poll(ctx context.Context, ownerID, pollKey string) (PollAnalytics, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return PollAnalytics{}, ErrPollNotFound
	}
	poll, snapshot, err := s.repo.PollSnapshot(ctx, owner, pollKey)
	if err != nil {
		return PollAnalytics{}, err
	}
	if poll == nil {
		return PollAnalytics{}, ErrPollNotFound
	}
	return buildPollAnalytics(poll, snapshot), nil
}

func buildPollAnalytics(poll *models.Poll, snapshot repository.AnalyticsSnapshot) PollAnalytics {
	result := PollAnalytics{Poll: TopPoll{ID: poll.ID.Hex(), Slug: poll.Slug, Question: poll.Question, Status: poll.Status}, Options: make([]OptionAnalytics, 0, len(poll.Options)), VotesOverTime: make([]DailyVotes, 0, 24), RecentVotes: make([]RecentVote, 0, 5)}
	counts := map[string]int64{}
	var maxVotes int64
	for _, vote := range snapshot.Votes {
		counts[vote.OptionID]++
		if counts[vote.OptionID] > maxVotes {
			maxVotes = counts[vote.OptionID]
		}
	}
	result.TotalVotes = int64(len(snapshot.Votes))
	result.Views = int64(len(snapshot.Views))
	result.Poll.TotalVotes = result.TotalVotes
	result.Poll.Views = result.Views
	if result.Views > 0 {
		result.ConversionRate = float64(result.TotalVotes) / float64(result.Views) * 100
	}
	result.VotesOverTime = hourlyVotes(snapshot.Votes, time.Now().UTC())
	optionNames := make(map[string]string, len(poll.Options))
	for _, option := range poll.Options {
		optionNames[option.ID] = option.Text
	}
	recent := append([]models.Vote(nil), snapshot.Votes...)
	sort.Slice(recent, func(i, j int) bool { return recent[i].CreatedAt.After(recent[j].CreatedAt) })
	if len(recent) > 5 {
		recent = recent[:5]
	}
	for _, vote := range recent {
		result.RecentVotes = append(result.RecentVotes, RecentVote{OptionName: optionNames[vote.OptionID], CreatedAt: vote.CreatedAt})
	}
	leaders := 0
	for _, option := range poll.Options {
		if counts[option.ID] == maxVotes && maxVotes > 0 {
			leaders++
		}
	}
	for _, option := range poll.Options {
		v := counts[option.ID]
		pct := float64(0)
		if result.TotalVotes > 0 {
			pct = float64(v) / float64(result.TotalVotes) * 100
		}
		result.Options = append(result.Options, OptionAnalytics{option.ID, option.Text, v, pct, maxVotes > 0 && v == maxVotes && leaders == 1})
	}
	return result
}

func hourlyVotes(votes []models.Vote, now time.Time) []DailyVotes {
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	counts := make(map[time.Time]int64, 24)
	for _, vote := range votes {
		hour := vote.CreatedAt.UTC().Truncate(time.Hour)
		if !hour.Before(start) && !hour.After(now.Truncate(time.Hour)) {
			counts[hour]++
		}
	}
	series := make([]DailyVotes, 0, 24)
	for i := 0; i < 24; i++ {
		hour := start.Add(time.Duration(i) * time.Hour)
		series = append(series, DailyVotes{Date: hour.Format(time.RFC3339), Votes: counts[hour]})
	}
	return series
}

func growth(current, previous int64) float64 { return growthFloat(float64(current), float64(previous)) }
func growthFloat(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return (current - previous) / previous * 100
}
