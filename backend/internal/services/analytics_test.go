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
func (f fixedAnalytics) PollSnapshot(context.Context, bson.ObjectID, bson.ObjectID) (*models.Poll, repository.AnalyticsSnapshot, error) {
	return f.poll, f.snapshot, nil
}

func TestPollAnalyticsZeroVotesReturnsZeroOptionsAndEmptySeries(t *testing.T) {
	poll := &models.Poll{ID: bson.ObjectID{1}, Slug: "poll1234", Question: "Zero votes?", Status: models.PollActive, Options: []models.PollOption{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}}
	service := NewAnalyticsService(fixedAnalytics{poll: poll, snapshot: repository.AnalyticsSnapshot{Polls: []models.Poll{*poll}}})
	result, err := service.Poll(context.Background(), bson.ObjectID{2}.Hex(), poll.ID.Hex())
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalVotes != 0 || result.Views != 0 || result.ConversionRate != 0 || len(result.Options) != 2 || len(result.VotesOverTime) != 0 || len(result.PeakHours) != 0 || len(result.DeviceSplit) != 0 || len(result.SourceSplit) != 0 {
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
