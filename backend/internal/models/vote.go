package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Vote struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"-"`
	VoteID    string        `bson:"voteId,omitempty" json:"-"`
	PollID    bson.ObjectID `bson:"pollId" json:"pollId"`
	OptionID  string        `bson:"optionId" json:"optionId"`
	CreatedAt time.Time     `bson:"createdAt" json:"createdAt"`
	VoterHash string        `bson:"voterHash" json:"-"`
}
