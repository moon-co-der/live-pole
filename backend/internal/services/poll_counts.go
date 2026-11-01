package services

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"live-polling/internal/models"
	"live-polling/internal/repository"
)

const pollCountsDefaultTTL = 365 * 24 * time.Hour
const pollRetentionBuffer = 30 * 24 * time.Hour

type PollCountsCache interface {
	Exists(context.Context, string) (bool, error)
	HGetAll(context.Context, string) (map[string]int64, error)
	HSetTTL(context.Context, string, map[string]string, time.Duration) error
}

type PollMetadataCache interface {
	Get(context.Context, string) (string, bool, error)
	Set(context.Context, string, string, time.Duration) error
}

func cachePollMetadata(ctx context.Context, cache PollMetadataCache, poll *models.Poll) error {
	data, err := json.Marshal(poll)
	if err != nil {
		return err
	}
	return cache.Set(ctx, "poll:slug:"+poll.Slug, string(data), pollCountsTTL(poll))
}

func readCachedPoll(ctx context.Context, cache PollMetadataCache, slug string) (*models.Poll, bool, error) {
	value, found, err := cache.Get(ctx, "poll:slug:"+slug)
	if err != nil || !found {
		return nil, found, err
	}
	var poll models.Poll
	if err := json.Unmarshal([]byte(value), &poll); err != nil {
		return nil, false, err
	}
	if poll.ID.IsZero() || poll.Slug != slug {
		return nil, false, nil
	}
	return &poll, true, nil
}

func pollCountsTTL(poll *models.Poll) time.Duration {
	if poll.EndsAt == nil {
		return pollCountsDefaultTTL
	}
	remaining := time.Until(*poll.EndsAt)
	if remaining < 0 {
		remaining = 0
	}
	return remaining + pollRetentionBuffer
}

func EnsurePollCounts(ctx context.Context, poll *models.Poll, cache PollCountsCache, votes repository.VoteStore) error {
	key := "poll:" + poll.ID.Hex() + ":counts"
	exists, err := cache.Exists(ctx, key)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	counts, err := votes.CountsByPoll(ctx, poll.ID)
	if err != nil {
		return err
	}
	fields := make(map[string]string, len(poll.Options)+1)
	for _, option := range poll.Options {
		fields[option.ID] = strconv.FormatInt(counts[option.ID], 10)
	}
	fields["total"] = strconv.FormatInt(counts["total"], 10)
	return cache.HSetTTL(ctx, key, fields, pollCountsTTL(poll))
}
