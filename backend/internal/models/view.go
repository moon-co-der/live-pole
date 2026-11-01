package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type View struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"-"`
	PollID    bson.ObjectID `bson:"pollId" json:"pollId"`
	CreatedAt time.Time     `bson:"createdAt" json:"createdAt"`
	Source    string        `bson:"source" json:"source"`
}
