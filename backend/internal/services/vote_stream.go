package services

import (
	"context"
	"errors"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/models"
	"live-polling/internal/redisx"
	"live-polling/internal/repository"
)

const voteStreamGroup = "livepoll-mongo-writers"

func RunVoteStreamWorker(ctx context.Context, redis *redisx.Client, polls repository.PollStore, votes repository.VoteStore, consumer string) {
	for ctx.Err() == nil {
		if err := redis.EnsureVoteStreamGroup(ctx, voteStreamGroup); err != nil {
			log.Printf("vote stream group setup failed: %v", err)
			if !waitFor(ctx, time.Second) {
				return
			}
			continue
		}
		claimed, err := redis.ClaimVoteStream(ctx, voteStreamGroup, consumer, 5*time.Second, 25)
		if err != nil {
			log.Printf("vote stream claim failed: %v", err)
		}
		for _, message := range claimed {
			persistStreamVote(ctx, redis, polls, votes, voteStreamGroup, message)
		}
		messages, err := redis.ReadVoteStream(ctx, voteStreamGroup, consumer, 25)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("vote stream read failed: %v", err)
			if !waitFor(ctx, time.Second) {
				return
			}
			continue
		}
		for _, message := range messages {
			persistStreamVote(ctx, redis, polls, votes, voteStreamGroup, message)
		}
		if len(messages) == 0 && len(claimed) == 0 && !waitFor(ctx, 300*time.Millisecond) {
			return
		}
	}
}

func persistStreamVote(ctx context.Context, redis *redisx.Client, polls repository.PollStore, votes repository.VoteStore, group string, message redisx.StreamMessage) {
	fields := message.Fields
	_, err := bson.ObjectIDFromHex(fields["pollId"])
	_, timeErr := time.Parse(time.RFC3339Nano, fields["createdAt"])
	if err != nil || timeErr != nil || fields["voteId"] == "" || fields["optionId"] == "" || fields["voterHash"] == "" {
		log.Printf("discarding malformed vote stream entry %s", message.ID)
		if ackErr := redis.AckVoteStream(ctx, group, message.ID); ackErr != nil {
			log.Printf("vote stream ack failed for %s: %v", message.ID, ackErr)
		}
		return
	}
	entry := redisx.VoteStreamEntry{VoteID: fields["voteId"], PollID: fields["pollId"], OptionID: fields["optionId"], CreatedAt: fields["createdAt"], VoterHash: fields["voterHash"]}
	_, err = PersistVoteEvent(ctx, polls, votes, entry)
	if err != nil {
		log.Printf("MongoDB vote write deferred for stream entry %s: %v", message.ID, err)
		return
	}
	if err := redis.AckVoteStream(ctx, group, message.ID); err != nil {
		log.Printf("vote stream ack failed for %s: %v", message.ID, err)
	}
}

func PersistVoteEvent(ctx context.Context, polls repository.PollStore, votes repository.VoteStore, entry redisx.VoteStreamEntry) (bool, error) {
	pollID, err := bson.ObjectIDFromHex(entry.PollID)
	if err != nil {
		return false, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, entry.CreatedAt)
	if err != nil {
		return false, err
	}
	if entry.VoteID == "" || entry.OptionID == "" || entry.VoterHash == "" {
		return false, errors.New("invalid vote stream event")
	}
	poll, err := polls.FindByID(ctx, pollID)
	if err != nil {
		return false, err
	}
	if poll == nil {
		return false, nil
	}
	vote := &models.Vote{VoteID: entry.VoteID, PollID: pollID, OptionID: entry.OptionID, CreatedAt: createdAt, VoterHash: entry.VoterHash}
	if err := votes.Create(ctx, vote); err != nil {
		if repository.IsDuplicateVote(err) {
			return false, nil
		}
		return false, err
	}
	if err := polls.IncrementVoteCount(ctx, pollID, entry.OptionID); err != nil {
		log.Printf("cached vote total update deferred for poll %s: %v", pollID.Hex(), err)
	}
	return true, nil
}

func waitFor(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
