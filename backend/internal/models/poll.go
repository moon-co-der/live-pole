package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type PollStatus string

const (
	PollDraft     PollStatus = "draft"
	PollActive    PollStatus = "active"
	PollCompleted PollStatus = "completed"
)

type PollOption struct {
	ID    string `bson:"id" json:"id"`
	Text  string `bson:"text" json:"text"`
	Votes int64  `bson:"votes" json:"votes"`
}

type Poll struct {
	ID            bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Owner         bson.ObjectID `bson:"owner" json:"owner"`
	Slug          string        `bson:"slug" json:"slug"`
	Question      string        `bson:"question" json:"question"`
	Options       []PollOption  `bson:"options" json:"options"`
	Status        PollStatus    `bson:"status" json:"status"`
	AllowMultiple bool          `bson:"allowMultiple" json:"allowMultiple"`
	EndsAt        *time.Time    `bson:"endsAt,omitempty" json:"endsAt,omitempty"`
	TotalVotes    int64         `bson:"totalVotes" json:"totalVotes"`
	Views         int64         `bson:"views" json:"views"`
	CreatedAt     time.Time     `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time     `bson:"updatedAt" json:"updatedAt"`
}

type PublicPollOption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type PublicPoll struct {
	Question string             `json:"question"`
	Options  []PublicPollOption `json:"options"`
	Status   PollStatus         `json:"status"`
}
