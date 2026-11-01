package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"

	"live-polling/internal/config"
	"live-polling/internal/models"
	"live-polling/internal/redisx"
)

const demoEmail = "analytics-demo@livepoll.local"
const demoPassword = "LivePollDemo2026!"

func main() {
	_ = godotenv.Load()
	uri := strings.TrimSpace(os.Getenv("MONGO_URI"))
	if uri == "" {
		log.Fatal("MONGO_URI is required")
	}
	client, err := config.ConnectMongo(uri)
	if err != nil {
		log.Fatal("MongoDB connection failed")
	}
	defer client.Disconnect(context.Background())
	db := client.Database("live_polling")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	users := db.Collection("users")
	var user models.User
	err = users.FindOne(ctx, bson.M{"email": demoEmail}).Decode(&user)
	hash, errHash := bcrypt.GenerateFromPassword([]byte(demoPassword), bcrypt.DefaultCost)
	if errHash != nil {
		log.Fatal(errHash)
	}
	if err == mongo.ErrNoDocuments {
		user = models.User{Email: demoEmail, PasswordHash: string(hash), CreatedAt: time.Now().UTC()}
		result, insertErr := users.InsertOne(ctx, user)
		if insertErr != nil {
			log.Fatal(insertErr)
		}
		user.ID = result.InsertedID.(bson.ObjectID)
	} else if err != nil {
		log.Fatal(err)
	} else {
		_, err = users.UpdateOne(ctx, bson.M{"_id": user.ID}, bson.M{"$set": bson.M{"passwordHash": string(hash)}})
		if err != nil {
			log.Fatal(err)
		}
	}
	polls := db.Collection("polls")
	voteCollection := db.Collection("votes")
	viewCollection := db.Collection("views")
	cursor, err := polls.Find(ctx, bson.M{"owner": user.ID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		log.Fatal(err)
	}
	var old []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cursor.All(ctx, &old); err != nil {
		log.Fatal(err)
	}
	_ = cursor.Close(ctx)
	ids := make([]bson.ObjectID, 0, len(old))
	for _, p := range old {
		ids = append(ids, p.ID)
	}
	if len(ids) > 0 {
		_, _ = voteCollection.DeleteMany(ctx, bson.M{"pollId": bson.M{"$in": ids}})
		_, _ = viewCollection.DeleteMany(ctx, bson.M{"pollId": bson.M{"$in": ids}})
		_, _ = polls.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}})
	}
	random := rand.New(rand.NewSource(20261101))
	now := time.Now().UTC()
	redisClient := redisx.NewClient(os.Getenv("REDIS_URL"))
	questions := []string{"Where should we host the next team meetup?", "Which feature should LivePoll build next?", "What is your preferred focus music?", "Which snack should we order?", "A poll waiting for its first vote"}
	choices := [][]string{{"Rooftop", "Garden", "Cafe"}, {"Poll templates", "Team spaces", "More charts"}, {"Lo-fi", "Instrumental", "Silence"}, {"Fruit", "Cookies", "Popcorn"}, {"Option A", "Option B"}}
	sizes := []int{220, 165, 95, 50, 0}
	for pollIndex, question := range questions {
		created := now.AddDate(0, 0, -random.Intn(26))
		pollID := bson.NewObjectID()
		poll := models.Poll{ID: pollID, Owner: user.ID, Slug: pollID.Hex()[16:], Question: question, Status: models.PollActive, CreatedAt: created, UpdatedAt: created, Options: make([]models.PollOption, len(choices[pollIndex]))}
		for i, text := range choices[pollIndex] {
			poll.Options[i] = models.PollOption{ID: fmt.Sprintf("option-%d", i+1), Text: text}
		}
		if _, err := polls.InsertOne(ctx, poll); err != nil {
			log.Fatal(err)
		}
		countMap := map[string]string{"total": "0"}
		for _, option := range poll.Options {
			countMap[option.ID] = "0"
		}
		for voteIndex := 0; voteIndex < sizes[pollIndex]; voteIndex++ {
			optionIndex := random.Intn(len(poll.Options))
			option := poll.Options[optionIndex]
			hour := random.Intn(24)
			daysAgo := random.Intn(30)
			at := now.AddDate(0, 0, -daysAgo)
			at = time.Date(at.Year(), at.Month(), at.Day(), hour, random.Intn(60), 0, 0, time.UTC)
			device := []string{"desktop", "mobile", "tablet"}[random.Intn(3)]
			source := []string{"direct", "localhost", "livepoll.example"}[random.Intn(3)]
			seedVoter := fmt.Sprintf("seed-%s-%d", poll.ID.Hex(), voteIndex)
			voterHash := sha256.Sum256([]byte(seedVoter))
			vote := models.Vote{VoteID: bson.NewObjectID().Hex(), PollID: poll.ID, OptionID: option.ID, CreatedAt: at, Device: device, Source: source, VoterHash: hex.EncodeToString(voterHash[:])}
			if _, err := voteCollection.InsertOne(ctx, vote); err != nil {
				log.Fatal(err)
			}
			countMap[option.ID] = fmt.Sprint(parse(countMap[option.ID]) + 1)
			countMap["total"] = fmt.Sprint(parse(countMap["total"]) + 1)
			poll.Options[optionIndex].Votes++
		}
		poll.TotalVotes = int64(sizes[pollIndex])
		viewCount := sizes[pollIndex] + random.Intn(sizes[pollIndex]/2+1) + 12
		for viewIndex := 0; viewIndex < viewCount; viewIndex++ {
			at := now.AddDate(0, 0, -random.Intn(30))
			at = time.Date(at.Year(), at.Month(), at.Day(), random.Intn(24), random.Intn(60), 0, 0, time.UTC)
			source := []string{"direct", "localhost", "livepoll.example"}[random.Intn(3)]
			if _, err := viewCollection.InsertOne(ctx, models.View{PollID: poll.ID, CreatedAt: at, Source: source}); err != nil {
				log.Fatal(err)
			}
		}
		poll.Views = int64(viewCount)
		if _, err := polls.UpdateOne(ctx, bson.M{"_id": poll.ID}, bson.M{"$set": bson.M{"options": poll.Options, "totalVotes": poll.TotalVotes, "views": poll.Views}}); err != nil {
			log.Fatal(err)
		}
		if redisClient != nil {
			if err := redisClient.HSetTTL(ctx, "poll:"+poll.ID.Hex()+":counts", countMap, 60*24*time.Hour); err != nil {
				log.Printf("Redis counts seed failed for %s: %v", poll.ID.Hex(), err)
			}
		}
	}
	log.Printf("Seeded analytics demo user %s, 5 polls and %d votes", demoEmail, totalVotes(sizes))
}

func parse(value string) int { var n int; _, _ = fmt.Sscan(value, &n); return n }
func totalVotes(values []int) int {
	n := 0
	for _, value := range values {
		n += value
	}
	return n
}
