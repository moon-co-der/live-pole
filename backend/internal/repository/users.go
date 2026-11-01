package repository

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"live-polling/internal/models"
)

type UserStore interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *models.User) error
	FindByEmail(context.Context, string) (*models.User, error)
	FindByID(context.Context, bson.ObjectID) (*models.User, error)
}

type UserRepository struct {
	collection *mongo.Collection
}

func NewUserRepository(collection *mongo.Collection) *UserRepository {
	return &UserRepository{collection: collection}
}

func (r *UserRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err == nil || !isIndexConflict(err) {
		return err
	}
	if r.hasEquivalentEmailIndex(ctx) {
		return nil
	}
	return err
}

func (r *UserRepository) hasEquivalentEmailIndex(ctx context.Context) bool {
	cursor, err := r.collection.Indexes().List(ctx)
	if err != nil {
		return false
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var index struct {
			Keys               bson.D   `bson:"key"`
			Unique             bool     `bson:"unique"`
			Sparse             bool     `bson:"sparse"`
			PartialFilter      bson.Raw `bson:"partialFilterExpression"`
			Collation          bson.Raw `bson:"collation"`
			ExpireAfterSeconds *int64   `bson:"expireAfterSeconds"`
		}
		if cursor.Decode(&index) != nil || len(index.Keys) != 1 {
			continue
		}
		if index.Keys[0].Key != "email" || !isAscendingIndexKey(index.Keys[0].Value) || !index.Unique || index.Sparse || index.PartialFilter != nil || index.Collation != nil || index.ExpireAfterSeconds != nil {
			continue
		}
		return true
	}
	return false
}

func isAscendingIndexKey(value any) bool {
	switch value := value.(type) {
	case int32:
		return value == 1
	case int64:
		return value == 1
	case int:
		return value == 1
	case float64:
		return value == 1
	default:
		return false
	}
}

func isIndexConflict(err error) bool {
	var serverErr mongo.ServerError
	if errors.As(err, &serverErr) && (serverErr.HasErrorCode(85) || serverErr.HasErrorCode(86)) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "indexoptionsconflict") || strings.Contains(message, "indexkeyspecsconflict")
}

func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	result, err := r.collection.InsertOne(ctx, user)
	if err != nil {
		return err
	}
	if id, ok := result.InsertedID.(bson.ObjectID); ok {
		user.ID = id
	}
	return nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := r.collection.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, mongo.ErrNoDocuments
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.User, error) {
	var user models.User
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, mongo.ErrNoDocuments
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func IsDuplicateEmail(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}
