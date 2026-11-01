package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"live-polling/internal/models"
	"live-polling/internal/repository"
)

type ReconcileCounts interface {
	HGetAll(context.Context, string) (map[string]int64, error)
	HSetTTL(context.Context, string, map[string]string, time.Duration) error
	TTL(context.Context, string) (time.Duration, error)
}

func ReconcilePollCounts(ctx context.Context, polls repository.PollStore, votes repository.VoteStore, cache ReconcileCounts) (int, error) {
	active, err := polls.ActivePolls(ctx)
	if err != nil {
		return 0, err
	}
	fixed := 0
	for _, poll := range active {
		databaseCounts, err := votes.CountsByPoll(ctx, poll.ID)
		if err != nil {
			return fixed, fmt.Errorf("read vote totals for poll %s: %w", poll.ID.Hex(), err)
		}
		key := "poll:" + poll.ID.Hex() + ":counts"
		cached, err := cache.HGetAll(ctx, key)
		if err != nil {
			return fixed, fmt.Errorf("read Redis counts for poll %s: %w", poll.ID.Hex(), err)
		}
		cacheTTL, err := cache.TTL(ctx, key)
		if err != nil {
			return fixed, fmt.Errorf("read Redis count TTL for poll %s: %w", poll.ID.Hex(), err)
		}
		expected := make(map[string]int64, len(poll.Options)+1)
		fields := make(map[string]string, len(poll.Options)+1)
		for _, option := range poll.Options {
			expected[option.ID] = databaseCounts[option.ID]
			fields[option.ID] = strconv.FormatInt(expected[option.ID], 10)
		}
		expected["total"] = databaseCounts["total"]
		fields["total"] = strconv.FormatInt(expected["total"], 10)
		updatedOptions := make([]models.PollOption, len(poll.Options))
		copy(updatedOptions, poll.Options)
		for i := range updatedOptions {
			updatedOptions[i].Votes = databaseCounts[updatedOptions[i].ID]
		}
		if err := polls.SyncVoteCounts(ctx, poll.ID, updatedOptions, databaseCounts["total"]); err != nil {
			return fixed, fmt.Errorf("repair MongoDB cached counts for poll %s: %w", poll.ID.Hex(), err)
		}
		if equalCounts(expected, cached) && cacheTTL > 0 {
			continue
		}
		if err := cache.HSetTTL(ctx, key, fields, pollCountsTTL(&poll)); err != nil {
			return fixed, fmt.Errorf("repair Redis counts for poll %s: %w", poll.ID.Hex(), err)
		}
		log.Printf("reconciled Redis counts for poll %s", poll.ID.Hex())
		fixed++
	}
	return fixed, nil
}

func equalCounts(expected, cached map[string]int64) bool {
	if len(expected) != len(cached) {
		return false
	}
	for key, value := range expected {
		if cached[key] != value {
			return false
		}
	}
	return true
}
