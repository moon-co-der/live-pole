package services

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/models"
	"live-polling/internal/repository"
)

type fixedAnalytics struct {
	snapshot repository.AnalyticsSnapshot
	poll     *models.Poll
}

func (f fixedAnalytics) OwnerSnapshot(context.Context, bson.ObjectID) (repository.AnalyticsSnapshot, error) {
	return f.snapshot, nil
}
func (f fixedAnalytics) PollSnapshot(context.Context, bson.ObjectID, string) (*models.Poll, repository.AnalyticsSnapshot, error) {
	return f.poll, f.snapshot, nil
}
func TestPollAnalyticsZeroVotesReturnsZeroOptionsAndEmptySeries(t *testing.T) {
	poll := &models.Poll{ID: bson.ObjectID{1}, Slug: "poll1234", Question: "Zero votes?", Status: models.PollActive, Options: []models.PollOption{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}}
	service := NewAnalyticsService(fixedAnalytics{poll: poll, snapshot: repository.AnalyticsSnapshot{Polls: []models.Poll{*poll}}})
	result, err := service.Poll(context.Background(), bson.ObjectID{2}.Hex(), poll.ID.Hex())
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalVotes != 0 || result.Views != 0 || result.ConversionRate != 0 || len(result.Options) != 2 || len(result.VotesOverTime) != 24 {
		t.Fatalf("unexpected zero-vote analytics: %+v", result)
	}
	for _, option := range result.Options {
		if option.Votes != 0 || option.Percentage != 0 {
			t.Fatalf("zero-vote option not zeroed: %+v", option)
		}
	}
}

func TestOverviewAndVotesOverTimeUseSeededEvents(t *testing.T) {
	now := time.Now().UTC()
	owner := bson.ObjectID{2}
	poll := models.Poll{ID: bson.ObjectID{1}, Owner: owner, Slug: "poll1234", Question: "Seeded?", Status: models.PollActive, CreatedAt: now.Add(-time.Hour)}
	snapshot := repository.AnalyticsSnapshot{Polls: []models.Poll{poll}, Votes: []models.Vote{{PollID: poll.ID, CreatedAt: now.Add(-time.Hour)}, {PollID: poll.ID, CreatedAt: now.Add(-2 * time.Hour)}, {PollID: poll.ID, CreatedAt: now.Add(-35 * 24 * time.Hour)}}, Views: []models.View{{PollID: poll.ID, CreatedAt: now.Add(-time.Hour)}, {PollID: poll.ID, CreatedAt: now.Add(-36 * 24 * time.Hour)}}}
	service := NewAnalyticsService(fixedAnalytics{snapshot: snapshot})
	overview, err := service.Overview(context.Background(), owner.Hex())
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalVotes.Value != 3 || overview.TotalVotes.Growth != 100 || overview.TotalViews.Value != 2 || overview.TotalPolls.Value != 1 {
		t.Fatalf("overview mismatched snapshot: %+v", overview)
	}
	series, err := service.VotesOverTime(context.Background(), owner.Hex(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 7 || series[6].Votes != 2 {
		t.Fatalf("7-day vote series: %+v", series)
	}
	if _, err := service.VotesOverTime(context.Background(), owner.Hex(), 8); err == nil {
		t.Fatal("invalid days were accepted")
	}
}

func TestOverviewIncludesLocalDayAndRecentCreatorActivity(t *testing.T) {
	now := time.Now().UTC()
	votedPoll := models.Poll{ID: bson.ObjectID{1}, Slug: "voted123", Question: "Lunch?", Status: models.PollCompleted, CreatedAt: now.Add(-time.Hour), Options: []models.PollOption{{ID: "burger", Text: "Burger"}}}
	openPoll := models.Poll{ID: bson.ObjectID{2}, Slug: "open1234", Question: "Drink?", Status: models.PollActive, CreatedAt: now, Options: []models.PollOption{{ID: "tea", Text: "Tea"}}}
	snapshot := repository.AnalyticsSnapshot{
		Polls: []models.Poll{votedPoll, openPoll},
		Votes: []models.Vote{
			{PollID: votedPoll.ID, OptionID: "burger", CreatedAt: now},
			{PollID: votedPoll.ID, OptionID: "burger", CreatedAt: now.Add(-48 * time.Hour)},
		},
	}
	service := NewAnalyticsService(fixedAnalytics{snapshot: snapshot})
	result, err := service.OverviewForTimezone(context.Background(), bson.ObjectID{3}.Hex(), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if result.OpenPolls != 1 || result.VotesToday != 1 || result.PollsWithoutVotes != 1 {
		t.Fatalf("unexpected dashboard totals: open=%d today=%d withoutVotes=%d", result.OpenPolls, result.VotesToday, result.PollsWithoutVotes)
	}
	if len(result.Polls) != 2 || result.Polls[0].ID != openPoll.ID.Hex() || result.Polls[1].TotalVotes != 2 || result.Polls[1].LastVoteAt == nil {
		t.Fatalf("unexpected dashboard polls: %+v", result.Polls)
	}
	if len(result.RecentActivity) != 2 || result.RecentActivity[0].OptionName != "Burger" || result.RecentActivity[0].PollTitle != "Lunch?" {
		t.Fatalf("unexpected recent activity: %+v", result.RecentActivity)
	}
}
