package services

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/models"
	"live-polling/internal/repository"
)

type AnalyticsStore interface {
	OwnerSnapshot(context.Context, bson.ObjectID) (repository.AnalyticsSnapshot, error)
	PollSnapshot(context.Context, bson.ObjectID, bson.ObjectID) (*models.Poll, repository.AnalyticsSnapshot, error)
	PollSnapshotBySlug(context.Context, bson.ObjectID, string) (*models.Poll, repository.AnalyticsSnapshot, error)
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
	TotalPolls GrowthStat `json:"totalPolls"`
	TotalVotes GrowthStat `json:"totalVotes"`
	TotalViews GrowthStat `json:"totalViews"`
	Engagement GrowthStat `json:"engagement"`
	PeriodDays int        `json:"periodDays"`
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
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return Overview{}, ErrPollNotFound
	}
	snapshot, err := s.repo.OwnerSnapshot(ctx, owner)
	if err != nil {
		return Overview{}, err
	}
	now := time.Now().UTC()
	current := now.AddDate(0, 0, -30)
	previous := now.AddDate(0, 0, -60)
	var pollsNow, pollsPrev, votesNow, votesPrev, viewsNow, viewsPrev int64
	for _, p := range snapshot.Polls {
		if !p.CreatedAt.Before(current) {
			pollsNow++
		} else if !p.CreatedAt.Before(previous) {
			pollsPrev++
		}
	}
	for _, v := range snapshot.Votes {
		if !v.CreatedAt.Before(current) {
			votesNow++
		} else if !v.CreatedAt.Before(previous) {
			votesPrev++
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
	return Overview{TotalPolls: GrowthStat{float64(len(snapshot.Polls)), growth(pollsNow, pollsPrev)}, TotalVotes: GrowthStat{float64(allVotes), growth(votesNow, votesPrev)}, TotalViews: GrowthStat{float64(allViews), growth(viewsNow, viewsPrev)}, Engagement: GrowthStat{engagement, growthFloat(currentEngagement, previousEngagement)}, PeriodDays: 30}, nil
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
type SplitCount struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}
type PollAnalytics struct {
	Poll           TopPoll           `json:"poll"`
	TotalVotes     int64             `json:"totalVotes"`
	Views          int64             `json:"views"`
	UniqueViewers  int64             `json:"uniqueViewers"`
	ConversionRate float64           `json:"conversionRate"`
	Options        []OptionAnalytics `json:"options"`
	VotesOverTime  []DailyVotes      `json:"votesOverTime"`
	PeakHours      []SplitCount      `json:"peakHours"`
	DeviceSplit    []SplitCount      `json:"deviceSplit"`
	SourceSplit    []SplitCount      `json:"sourceSplit"`
	CountrySplit   []SplitCount      `json:"countrySplit"`
	PeakDays       []SplitCount      `json:"peakDays"`
	PeakHour       string            `json:"peakHour"`
	PeakDay        string            `json:"peakDay"`
	VotesLastHour  int64             `json:"votesLastHour"`
	AveragePerHour float64           `json:"averageVotesPerHour"`
	Activity       []VoteActivity    `json:"activity"`
}

type VoteActivity struct {
	Option string    `json:"option"`
	At     time.Time `json:"at"`
}
type VoteTimelineRow struct {
	At                              time.Time
	Option, Source, Device, Country string
}

func (s *AnalyticsService) Poll(ctx context.Context, ownerID, pollID string) (PollAnalytics, error) {
	return s.PollInterval(ctx, ownerID, pollID, "day")
}

func (s *AnalyticsService) PollInterval(ctx context.Context, ownerID, pollID, interval string) (PollAnalytics, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return PollAnalytics{}, ErrPollNotFound
	}
	id, err := bson.ObjectIDFromHex(pollID)
	if err != nil {
		return PollAnalytics{}, ErrPollNotFound
	}
	if interval != "minute" && interval != "hour" && interval != "day" {
		return PollAnalytics{}, ErrInvalidPoll
	}
	poll, snapshot, err := s.repo.PollSnapshot(ctx, owner, id)
	if err != nil {
		return PollAnalytics{}, err
	}
	if poll == nil {
		return PollAnalytics{}, ErrPollNotFound
	}
	return buildPollAnalytics(poll, snapshot, interval), nil
}

func (s *AnalyticsService) PollBySlug(ctx context.Context, ownerID, slug, interval string) (PollAnalytics, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return PollAnalytics{}, ErrPollNotFound
	}
	if interval != "minute" && interval != "hour" && interval != "day" {
		return PollAnalytics{}, ErrInvalidPoll
	}
	poll, snapshot, err := s.repo.PollSnapshotBySlug(ctx, owner, slug)
	if err != nil {
		return PollAnalytics{}, err
	}
	if poll == nil {
		return PollAnalytics{}, ErrPollNotFound
	}
	return buildPollAnalytics(poll, snapshot, interval), nil
}

func (s *AnalyticsService) VoteTimelineBySlug(ctx context.Context, ownerID, slug string) ([]VoteTimelineRow, error) {
	owner, err := bson.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, ErrPollNotFound
	}
	poll, snapshot, err := s.repo.PollSnapshotBySlug(ctx, owner, slug)
	if err != nil {
		return nil, err
	}
	if poll == nil {
		return nil, ErrPollNotFound
	}
	options := map[string]string{}
	for _, option := range poll.Options {
		options[option.ID] = option.Text
	}
	rows := make([]VoteTimelineRow, 0, len(snapshot.Votes))
	for _, vote := range snapshot.Votes {
		rows = append(rows, VoteTimelineRow{At: vote.CreatedAt, Option: nonempty(options[vote.OptionID], "Unknown option"), Source: nonempty(vote.Source, "direct"), Device: nonempty(vote.Device, "unknown"), Country: nonempty(vote.Country, "Unknown")})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].At.Before(rows[j].At) })
	return rows, nil
}

func buildPollAnalytics(poll *models.Poll, snapshot repository.AnalyticsSnapshot, interval string) PollAnalytics {
	result := PollAnalytics{Poll: TopPoll{ID: poll.ID.Hex(), Slug: poll.Slug, Question: poll.Question, Status: poll.Status}, Options: make([]OptionAnalytics, 0, len(poll.Options)), VotesOverTime: []DailyVotes{}, PeakHours: []SplitCount{}, DeviceSplit: []SplitCount{}, SourceSplit: []SplitCount{}, CountrySplit: []SplitCount{}, PeakDays: []SplitCount{}, Activity: []VoteActivity{}}
	counts := map[string]int64{}
	devices, sources, hours, weekdays, countries := map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}
	buckets := map[string]int64{}
	optionText := map[string]string{}
	for _, option := range poll.Options {
		optionText[option.ID] = option.Text
	}
	var maxVotes int64
	now := time.Now().UTC()
	for _, vote := range snapshot.Votes {
		counts[vote.OptionID]++
		if counts[vote.OptionID] > maxVotes {
			maxVotes = counts[vote.OptionID]
		}
		devices[analyticsDevice(vote.Device)]++
		sources[analyticsSource(vote.Source)]++
		hour := vote.CreatedAt.UTC().Hour()
		hours[itoa(hour)]++
		weekdays[vote.CreatedAt.UTC().Weekday().String()]++
		countries[nonempty(vote.Country, "Unknown")]++
		switch interval {
		case "minute":
			if vote.CreatedAt.After(now.Add(-time.Hour)) {
				buckets[vote.CreatedAt.UTC().Format("15:04")]++
			}
		case "hour":
			if vote.CreatedAt.After(now.Add(-24 * time.Hour)) {
				buckets[vote.CreatedAt.UTC().Format("Jan 02 15:00")]++
			}
		default:
			buckets[vote.CreatedAt.UTC().Format("2006-01-02")]++
		}
		if vote.CreatedAt.After(now.Add(-time.Hour)) {
			result.VotesLastHour++
		}
		result.Activity = append(result.Activity, VoteActivity{Option: nonempty(optionText[vote.OptionID], "Option"), At: vote.CreatedAt})
	}
	sort.Slice(result.Activity, func(i, j int) bool { return result.Activity[i].At.After(result.Activity[j].At) })
	if len(result.Activity) > 20 {
		result.Activity = result.Activity[:20]
	}
	result.TotalVotes = int64(len(snapshot.Votes))
	result.Views = int64(len(snapshot.Views))
	seenVisitors := map[string]struct{}{}
	for index, view := range snapshot.Views {
		key := view.VisitorHash
		if key == "" {
			key = "legacy-view-" + strconv.Itoa(index)
		}
		seenVisitors[key] = struct{}{}
	}
	result.UniqueViewers = int64(len(seenVisitors))
	result.Poll.TotalVotes = result.TotalVotes
	result.Poll.Views = result.Views
	if result.Views > 0 {
		result.ConversionRate = float64(result.TotalVotes) / float64(result.Views) * 100
	}
	hoursRunning := now.Sub(poll.CreatedAt).Hours()
	if hoursRunning < 1 {
		hoursRunning = 1
	}
	result.AveragePerHour = float64(result.TotalVotes) / hoursRunning
	for _, option := range poll.Options {
		v := counts[option.ID]
		pct := float64(0)
		if result.TotalVotes > 0 {
			pct = float64(v) / float64(result.TotalVotes) * 100
		}
		result.Options = append(result.Options, OptionAnalytics{option.ID, option.Text, v, pct, v == maxVotes && maxVotes > 0})
	}
	for key, count := range buckets {
		result.VotesOverTime = append(result.VotesOverTime, DailyVotes{key, count})
	}
	sort.Slice(result.VotesOverTime, func(i, j int) bool { return result.VotesOverTime[i].Date < result.VotesOverTime[j].Date })
	result.PeakHour = busiest(hours, 24, func(i int) string { return strconv.Itoa(i) }, "hour")
	result.PeakDay = busiestNamed(weekdays, []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"})
	if result.TotalVotes > 0 {
		result.PeakHours = make([]SplitCount, 0, 24)
		for hour := 0; hour < 24; hour++ {
			label := strconv.Itoa(hour)
			result.PeakHours = append(result.PeakHours, SplitCount{Label: label, Count: hours[label]})
		}
	}
	result.DeviceSplit = mapSplit(devices)
	result.SourceSplit = mapSplit(sources)
	result.CountrySplit = mapSplit(countries)
	result.PeakDays = mapSplit(weekdays)
	return result
}

func busiest(values map[string]int64, count int, label func(int) string, unit string) string {
	var best string
	var max int64
	for i := 0; i < count; i++ {
		key := label(i)
		if values[key] > max {
			max, best = values[key], key
		}
	}
	if best == "" {
		return "No votes yet"
	}
	if unit == "hour" {
		return best + ":00 UTC"
	}
	return best
}
func busiestNamed(values map[string]int64, names []string) string {
	best := "No votes yet"
	var max int64
	for _, name := range names {
		if values[name] > max {
			max, best = values[name], name
		}
	}
	return best
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
func mapSplit(values map[string]int64) []SplitCount {
	out := make([]SplitCount, 0, len(values))
	for k, v := range values {
		out = append(out, SplitCount{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
func nonempty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func analyticsDevice(value string) string {
	switch strings.ToLower(value) {
	case "mobile", "desktop", "tablet":
		return strings.ToLower(value)
	default:
		return "unknown"
	}
}
func analyticsSource(value string) string {
	host := strings.ToLower(strings.TrimSpace(value))
	switch {
	case host == "", host == "direct":
		return "direct"
	case strings.Contains(host, "whatsapp") || strings.Contains(host, "wa.me"):
		return "whatsapp"
	case strings.Contains(host, "instagram"):
		return "instagram"
	case strings.Contains(host, "twitter") || strings.Contains(host, "x.com") || strings.Contains(host, "t.co") || host == "twitter/x":
		return "twitter/x"
	default:
		return "other"
	}
}
func itoa(value int) string { return strconv.Itoa(value) }
