package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"live-polling/internal/models"
)

type PollListFilter struct {
	Owner  bson.ObjectID
	Status string
	Search string
	Sort   string
	Skip   int64
	Limit  int64
}

type PollStore interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *models.Poll) error
	DeleteByID(context.Context, bson.ObjectID) error
	FindOwned(context.Context, bson.ObjectID, bson.ObjectID) (*models.Poll, error)
	FindOwnedBySlug(context.Context, string, bson.ObjectID) (*models.Poll, error)
	FindByID(context.Context, bson.ObjectID) (*models.Poll, error)
	List(context.Context, PollListFilter) ([]models.Poll, int64, error)
	FindBySlug(context.Context, string) (*models.Poll, error)
	IncrementViews(context.Context, bson.ObjectID) error
	IncrementVoteCount(context.Context, bson.ObjectID, string) error
	CloseOwned(context.Context, bson.ObjectID, bson.ObjectID) error
	ActivePolls(context.Context) ([]models.Poll, error)
	SyncVoteCounts(context.Context, bson.ObjectID, []models.PollOption, int64) error
	DeleteOwned(context.Context, bson.ObjectID, bson.ObjectID) error
	FindExpired(context.Context, time.Time) ([]models.Poll, error)
	CompleteExpired(context.Context, bson.ObjectID, time.Time) (bool, error)
}

type VoteStore interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *models.Vote) error
	CountsByPoll(context.Context, bson.ObjectID) (map[string]int64, error)
	DeleteByPoll(context.Context, bson.ObjectID) error
	VotesByPoll(context.Context, bson.ObjectID) ([]models.Vote, error)
}

type ViewStore interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *models.View) error
	ByPolls(context.Context, []bson.ObjectID) ([]models.View, error)
	DeleteByPoll(context.Context, bson.ObjectID) error
}

type VoteRepository struct {
	collection *mongo.Collection
}

func NewVoteRepository(collection *mongo.Collection) *VoteRepository {
	return &VoteRepository{collection: collection}
}

func (r *VoteRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "pollId", Value: 1}, {Key: "voterHash", Value: 1}}, Options: options.Index().SetUnique(true).SetName("poll_voter_unique")},
		{Keys: bson.D{{Key: "pollId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("poll_created_idx")},
		{Keys: bson.D{{Key: "voteId", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true).SetName("vote_id_unique")},
	})
	return err
}

func (r *VoteRepository) Create(ctx context.Context, vote *models.Vote) error {
	_, err := r.collection.InsertOne(ctx, vote)
	return err
}

func (r *VoteRepository) DeleteByPoll(ctx context.Context, pollID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"pollId": pollID})
	return err
}

func (r *VoteRepository) VotesByPoll(ctx context.Context, pollID bson.ObjectID) ([]models.Vote, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"pollId": pollID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
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

type ViewRepository struct{ collection *mongo.Collection }

func NewViewRepository(collection *mongo.Collection) *ViewRepository {
	return &ViewRepository{collection: collection}
}
func (r *ViewRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "pollId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("view_poll_created_idx")}})
	return err
}
func (r *ViewRepository) Create(ctx context.Context, view *models.View) error {
	_, err := r.collection.InsertOne(ctx, view)
	return err
}
func (r *ViewRepository) DeleteByPoll(ctx context.Context, pollID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"pollId": pollID})
	return err
}
func (r *ViewRepository) ByPolls(ctx context.Context, ids []bson.ObjectID) ([]models.View, error) {
	if len(ids) == 0 {
		return []models.View{}, nil
	}
	cursor, err := r.collection.Find(ctx, bson.M{"pollId": bson.M{"$in": ids}})
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

func (r *VoteRepository) CountsByPoll(ctx context.Context, pollID bson.ObjectID) (map[string]int64, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"pollId": pollID}}},
		bson.D{{Key: "$group", Value: bson.M{"_id": "$optionId", "votes": bson.M{"$sum": 1}}}},
	}
	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		OptionID string `bson:"_id"`
		Votes    int64  `bson:"votes"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows)+1)
	var total int64
	for _, row := range rows {
		counts[row.OptionID] = row.Votes
		total += row.Votes
	}
	counts["total"] = total
	return counts, nil
}

type PollRepository struct {
	collection *mongo.Collection
}

func NewPollRepository(collection *mongo.Collection) *PollRepository {
	return &PollRepository{collection: collection}
}

func (r *PollRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true).SetName("slug_unique")},
		{Keys: bson.D{{Key: "owner", Value: 1}}, Options: options.Index().SetName("owner_idx")},
		{Keys: bson.D{{Key: "owner", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("owner_created_idx")},
	})
	return err
}

func (r *PollRepository) Create(ctx context.Context, poll *models.Poll) error {
	result, err := r.collection.InsertOne(ctx, poll)
	if err != nil {
		return err
	}
	if id, ok := result.InsertedID.(bson.ObjectID); ok {
		poll.ID = id
	}
	return nil
}

func (r *PollRepository) DeleteByID(ctx context.Context, id bson.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *PollRepository) DeleteOwned(ctx context.Context, id, owner bson.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id, "owner": owner})
	return err
}
func (r *PollRepository) FindExpired(ctx context.Context, now time.Time) ([]models.Poll, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"status": models.PollActive, "endsAt": bson.M{"$lte": now}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	items := make([]models.Poll, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}
func (r *PollRepository) CompleteExpired(ctx context.Context, id bson.ObjectID, now time.Time) (bool, error) {
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "status": models.PollActive, "endsAt": bson.M{"$lte": now}}, bson.M{"$set": bson.M{"status": models.PollCompleted, "updatedAt": now}})
	return result.ModifiedCount == 1, err
}

func (r *PollRepository) FindOwned(ctx context.Context, id, owner bson.ObjectID) (*models.Poll, error) {
	var poll models.Poll
	err := r.collection.FindOne(ctx, bson.M{"_id": id, "owner": owner}).Decode(&poll)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &poll, nil
}

func (r *PollRepository) FindOwnedBySlug(ctx context.Context, slug string, owner bson.ObjectID) (*models.Poll, error) {
	var poll models.Poll
	err := r.collection.FindOne(ctx, bson.M{"slug": slug, "owner": owner}).Decode(&poll)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &poll, nil
}

func (r *PollRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.Poll, error) {
	var poll models.Poll
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&poll)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &poll, nil
}

func (r *PollRepository) FindBySlug(ctx context.Context, slug string) (*models.Poll, error) {
	var poll models.Poll
	err := r.collection.FindOne(ctx, bson.M{"slug": slug}).Decode(&poll)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &poll, nil
}

func (r *PollRepository) IncrementViews(ctx context.Context, id bson.ObjectID) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": bson.M{"views": 1}})
	return err
}

func (r *PollRepository) IncrementVoteCount(ctx context.Context, id bson.ObjectID, optionID string) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "options.id": optionID},
		bson.M{"$inc": bson.M{"totalVotes": 1, "options.$.votes": 1}},
	)
	return err
}

func (r *PollRepository) CloseOwned(ctx context.Context, id, owner bson.ObjectID) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "owner": owner}, bson.M{"$set": bson.M{"status": models.PollCompleted, "updatedAt": time.Now().UTC()}})
	return err
}

func (r *PollRepository) ActivePolls(ctx context.Context) ([]models.Poll, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"status": models.PollActive})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	polls := make([]models.Poll, 0)
	if err := cursor.All(ctx, &polls); err != nil {
		return nil, err
	}
	return polls, nil
}

func (r *PollRepository) SyncVoteCounts(ctx context.Context, id bson.ObjectID, options []models.PollOption, total int64) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"options": options, "totalVotes": total}})
	return err
}

func (r *PollRepository) List(ctx context.Context, filter PollListFilter) ([]models.Poll, int64, error) {
	query := bson.M{"owner": filter.Owner}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.Search != "" {
		query["question"] = bson.M{"$regex": regexp.QuoteMeta(filter.Search), "$options": "i"}
	}
	total, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	sort := bson.D{{Key: "createdAt", Value: -1}}
	if filter.Sort == "votes" {
		sort = bson.D{{Key: "totalVotes", Value: -1}, {Key: "createdAt", Value: -1}}
	}
	findOptions := options.Find().SetSort(sort).SetSkip(filter.Skip).SetLimit(filter.Limit)
	cursor, err := r.collection.Find(ctx, query, findOptions)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	polls := make([]models.Poll, 0)
	if err := cursor.All(ctx, &polls); err != nil {
		return nil, 0, err
	}
	return polls, total, nil
}

func IsDuplicateSlug(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}

func IsDuplicateVote(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}
