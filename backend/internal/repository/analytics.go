package repository

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"live-polling/internal/models"
)

type AnalyticsSnapshot struct {
	Polls []models.Poll
	Votes []models.Vote
	Views []models.View
}

type AnalyticsRepository struct{ polls, votes, views *mongo.Collection }

func NewAnalyticsRepository(db *mongo.Database) *AnalyticsRepository {
	return &AnalyticsRepository{polls: db.Collection("polls"), votes: db.Collection("votes"), views: db.Collection("views")}
}

func (r *AnalyticsRepository) OwnerSnapshot(ctx context.Context, owner bson.ObjectID) (AnalyticsSnapshot, error) {
	cursor, err := r.polls.Find(ctx, bson.M{"owner": owner}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return AnalyticsSnapshot{}, err
	}
	defer cursor.Close(ctx)
	polls := make([]models.Poll, 0)
	if err := cursor.All(ctx, &polls); err != nil {
		return AnalyticsSnapshot{}, err
	}
	ids := make([]bson.ObjectID, 0, len(polls))
	for _, p := range polls {
		ids = append(ids, p.ID)
	}
	votes, err := findVotes(ctx, r.votes, ids)
	if err != nil {
		return AnalyticsSnapshot{}, err
	}
	views, err := findViews(ctx, r.views, ids)
	if err != nil {
		return AnalyticsSnapshot{}, err
	}
	return AnalyticsSnapshot{Polls: polls, Votes: votes, Views: views}, nil
}

func (r *AnalyticsRepository) PollSnapshot(ctx context.Context, owner, id bson.ObjectID) (*models.Poll, AnalyticsSnapshot, error) {
	var poll models.Poll
	err := r.polls.FindOne(ctx, bson.M{"_id": id, "owner": owner}).Decode(&poll)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, AnalyticsSnapshot{}, nil
	}
	if err != nil {
		return nil, AnalyticsSnapshot{}, err
	}
	return r.snapshotForPoll(ctx, &poll)
}

func (r *AnalyticsRepository) PollSnapshotBySlug(ctx context.Context, owner bson.ObjectID, slug string) (*models.Poll, AnalyticsSnapshot, error) {
	var poll models.Poll
	err := r.polls.FindOne(ctx, bson.M{"slug": slug, "owner": owner}).Decode(&poll)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, AnalyticsSnapshot{}, nil
	}
	if err != nil {
		return nil, AnalyticsSnapshot{}, err
	}
	return r.snapshotForPoll(ctx, &poll)
}

func (r *AnalyticsRepository) snapshotForPoll(ctx context.Context, poll *models.Poll) (*models.Poll, AnalyticsSnapshot, error) {
	id := poll.ID
	votes, err := findVotes(ctx, r.votes, []bson.ObjectID{id})
	if err != nil {
		return nil, AnalyticsSnapshot{}, err
	}
	views, err := findViews(ctx, r.views, []bson.ObjectID{id})
	if err != nil {
		return nil, AnalyticsSnapshot{}, err
	}
	return poll, AnalyticsSnapshot{Polls: []models.Poll{*poll}, Votes: votes, Views: views}, nil
}

func findVotes(ctx context.Context, collection *mongo.Collection, ids []bson.ObjectID) ([]models.Vote, error) {
	if len(ids) == 0 {
		return []models.Vote{}, nil
	}
	cursor, err := collection.Find(ctx, bson.M{"pollId": bson.M{"$in": ids}}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	items := make([]models.Vote, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}
func findViews(ctx context.Context, collection *mongo.Collection, ids []bson.ObjectID) ([]models.View, error) {
	if len(ids) == 0 {
		return []models.View{}, nil
	}
	cursor, err := collection.Find(ctx, bson.M{"pollId": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	items := make([]models.View, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}
