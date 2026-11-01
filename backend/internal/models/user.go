package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Email        string        `bson:"email" json:"email"`
	PasswordHash string        `bson:"passwordHash" json:"-"`
	CreatedAt    time.Time     `bson:"createdAt" json:"createdAt"`
}

type PublicUser struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

func (u User) Public() PublicUser {
	return PublicUser{ID: u.ID.Hex(), Email: u.Email, CreatedAt: u.CreatedAt}
}
