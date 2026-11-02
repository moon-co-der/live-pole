package redisx

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const countsKeyTTL = 365 * 24 * time.Hour

const rateLimitScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`

const voteScript = `
local selected = redis.call('HGET', KEYS[2], ARGV[3])
if not selected then return {-1} end
local optionCount = tonumber(ARGV[2])
if not redis.call('SET', KEYS[1], '1', 'NX', 'EX', ARGV[1]) then return {0} end
local optionID = ARGV[3]
redis.call('HINCRBY', KEYS[2], optionID, 1)
local total = redis.call('HINCRBY', KEYS[2], 'total', 1)
redis.call('EXPIRE', KEYS[2], ARGV[1])
local result = {1, total}
for i = 4, 3 + optionCount do
  table.insert(result, tonumber(redis.call('HGET', KEYS[2], ARGV[i]) or '0'))
end
local eventIndex = 4 + optionCount
redis.call('XADD', KEYS[3], '*',
  'voteId', ARGV[eventIndex], 'pollId', ARGV[eventIndex + 1],
  'optionId', ARGV[eventIndex + 2], 'createdAt', ARGV[eventIndex + 3],
  'voterHash', ARGV[eventIndex + 4])
return result
`

const voteStreamKey = "votes:stream"

type VoteStreamEntry struct {
	ID        string
	PollID    string
	OptionID  string
	VoteID    string
	CreatedAt string
	VoterHash string
}

type StreamMessage struct {
	ID     string
	Fields map[string]string
}

type Client struct {
	url string
}

func NewClient(rawURL string) *Client { return &Client{url: rawURL} }

func Ping(ctx context.Context, rawURL string) error {
	return withConnection(ctx, rawURL, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := command(conn, reader, "PING")
		if err != nil {
			return err
		}
		if response != "+PONG" {
			return errors.New("unexpected Redis PING response")
		}
		return nil
	})
}

func (c *Client) HSet(ctx context.Context, key string, values map[string]string) error {
	return c.HSetTTL(ctx, key, values, countsKeyTTL)
}

func (c *Client) HSetTTL(ctx context.Context, key string, values map[string]string, ttl time.Duration) error {
	if len(values) == 0 {
		return errors.New("Redis HSET requires at least one field")
	}
	fields := make([]string, 0, len(values))
	for field := range values {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	args := []string{"HSET", key}
	for _, field := range fields {
		args = append(args, field, values[field])
	}
	return withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := command(conn, reader, args...)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(response, ":") {
			return errors.New("unexpected Redis HSET response")
		}
		if _, err := command(conn, reader, "EXPIRE", key, strconv.FormatInt(ttlSeconds(ttl), 10)); err != nil {
			return err
		}
		return nil
	})
}

func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := command(conn, reader, "SET", key, value, "EX", strconv.FormatInt(ttlSeconds(ttl), 10))
		if err != nil {
			return err
		}
		if response != "+OK" {
			return errors.New("unexpected Redis SET response")
		}
		return nil
	})
}

func (c *Client) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	var found bool
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "GET", key)
		if err != nil {
			return err
		}
		if response == nil {
			return nil
		}
		text, ok := response.(string)
		if !ok {
			return errors.New("unexpected Redis GET response")
		}
		value, found = text, true
		return nil
	})
	return value, found, err
}

func (c *Client) GetDel(ctx context.Context, key string) (string, bool, error) {
	var value string
	var found bool
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "GETDEL", key)
		if err != nil {
			return err
		}
		if response == nil {
			return nil
		}
		text, ok := response.(string)
		if !ok {
			return errors.New("unexpected Redis GETDEL response")
		}
		value, found = text, true
		return nil
	})
	return value, found, err
}

func (c *Client) DeletePollKeys(ctx context.Context, pollID, slug string) error {
	keys := []string{"poll:slug:" + slug}
	for _, pattern := range []string{"poll:" + pollID + ":*", "voted:" + pollID + ":*", "viewed:" + pollID + ":*"} {
		cursor := "0"
		for {
			var next string
			var found []string
			err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
				response, err := commandValue(conn, reader, "SCAN", cursor, "MATCH", pattern, "COUNT", "100")
				if err != nil {
					return err
				}
				items, ok := response.([]any)
				if !ok || len(items) != 2 {
					return errors.New("unexpected Redis SCAN response")
				}
				var valid bool
				next, valid = items[0].(string)
				if !valid {
					return errors.New("invalid Redis SCAN cursor")
				}
				entries, valid := items[1].([]any)
				if !valid {
					return errors.New("invalid Redis SCAN keys")
				}
				for _, item := range entries {
					if key, ok := item.(string); ok {
						found = append(found, key)
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			keys = append(keys, found...)
			cursor = next
			if cursor == "0" {
				break
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		args := append([]string{"DEL"}, keys...)
		_, err := commandValue(conn, reader, args...)
		return err
	})
}

func (c *Client) AllowRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	var allowed bool
	var retryAfter time.Duration
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "EVAL", rateLimitScript, "1", key, strconv.FormatInt(ttlSeconds(window), 10))
		if err != nil {
			return err
		}
		items, ok := response.([]any)
		if !ok || len(items) != 2 {
			return errors.New("unexpected Redis rate-limit response")
		}
		count, countOK := items[0].(int64)
		ttl, ttlOK := items[1].(int64)
		if !countOK || !ttlOK {
			return errors.New("invalid Redis rate-limit response")
		}
		allowed = count <= int64(limit)
		if ttl > 0 {
			retryAfter = time.Duration(ttl) * time.Second
		}
		return nil
	})
	return allowed, retryAfter, err
}

func (c *Client) Publish(ctx context.Context, channel, message string) error {
	return withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := command(conn, reader, "PUBLISH", channel, message)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(response, ":") {
			return errors.New("unexpected Redis PUBLISH response")
		}
		return nil
	})
}

func (c *Client) SubscribePattern(ctx context.Context, pattern string, onMessage func(channel, message string)) error {
	conn, reader, err := c.openSubscriberConnection(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-finished:
		}
	}()
	defer close(finished)
	if _, err := commandValue(conn, reader, "PSUBSCRIBE", pattern); err != nil {
		return err
	}
	for {
		response, err := readResponse(reader)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		items, ok := response.([]any)
		if !ok || len(items) != 4 {
			continue
		}
		kind, _ := items[0].(string)
		channel, _ := items[2].(string)
		message, _ := items[3].(string)
		if kind == "pmessage" && channel != "" {
			onMessage(channel, message)
		}
	}
}

func (c *Client) openSubscriberConnection(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(c.url)
	if err != nil || u.Host == "" || (u.Scheme != "redis" && u.Scheme != "rediss") {
		return nil, nil, errors.New("REDIS_URL must be a " + "redis" + ":// or " + "rediss" + ":// URL")
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	var conn net.Conn
	if u.Scheme == "rediss" {
		conn, err = tls.DialWithDialer(dialer, "tcp", u.Host, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", u.Host)
	}
	if err != nil {
		return nil, nil, err
	}
	reader := bufio.NewReader(conn)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := authenticate(conn, reader, u); err != nil {
		conn.Close()
		return nil, nil, err
	}
	database := strings.TrimPrefix(u.EscapedPath(), "/")
	if database != "" && database != "0" {
		if _, err := strconv.Atoi(database); err != nil {
			conn.Close()
			return nil, nil, errors.New("Redis database path must be a number")
		}
		response, err := command(conn, reader, "SELECT", database)
		if err != nil || response != "+OK" {
			conn.Close()
			if err != nil {
				return nil, nil, err
			}
			return nil, nil, errors.New("Redis database selection failed")
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, reader, nil
}

func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := command(conn, reader, "EXISTS", key)
		if err != nil {
			return err
		}
		exists = response == ":1"
		return nil
	})
	return exists, err
}

func (c *Client) HGetAll(ctx context.Context, key string) (map[string]int64, error) {
	counts := make(map[string]int64)
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "HGETALL", key)
		if err != nil {
			return err
		}
		items, ok := response.([]any)
		if !ok || len(items)%2 != 0 {
			return errors.New("unexpected Redis HGETALL response")
		}
		for i := 0; i < len(items); i += 2 {
			field, fieldOK := items[i].(string)
			value, valueOK := items[i+1].(string)
			if !fieldOK || !valueOK {
				return errors.New("invalid Redis HGETALL field")
			}
			count, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			counts[field] = count
		}
		return nil
	})
	return counts, err
}

func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	var ttl time.Duration
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "TTL", key)
		if err != nil {
			return err
		}
		seconds, ok := response.(int64)
		if !ok {
			return errors.New("unexpected Redis TTL response")
		}
		if seconds >= 0 {
			ttl = time.Duration(seconds) * time.Second
		}
		return nil
	})
	return ttl, err
}

func (c *Client) SetNX(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	var created bool
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "SET", key, "1", "NX", "EX", strconv.FormatInt(int64(ttl.Seconds()), 10))
		if err != nil {
			return err
		}
		created = isOKResponse(response)
		return nil
	})
	return created, err
}

func (c *Client) SetNXValue(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	var created bool
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "SET", key, value, "NX", "EX", strconv.FormatInt(ttlSeconds(ttl), 10))
		if err != nil {
			return err
		}
		created = isOKResponse(response)
		return nil
	})
	return created, err
}

type VoteResult struct {
	Counts map[string]int64
	Total  int64
}

func (c *Client) CastVoteWithTTL(ctx context.Context, voterKey, countsKey string, optionID string, optionIDs []string, ttl time.Duration, entry VoteStreamEntry) (VoteResult, bool, error) {
	result := VoteResult{Counts: make(map[string]int64, len(optionIDs))}
	args := []string{"EVAL", voteScript, "3", voterKey, countsKey, voteStreamKey, strconv.FormatInt(ttlSeconds(ttl), 10), strconv.Itoa(len(optionIDs)), optionID}
	args = append(args, optionIDs...)
	args = append(args, entry.VoteID, entry.PollID, entry.OptionID, entry.CreatedAt, entry.VoterHash)
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, args...)
		if err != nil {
			return err
		}
		items, ok := response.([]any)
		if !ok || len(items) == 0 {
			return errors.New("unexpected Redis vote response")
		}
		status, ok := items[0].(int64)
		if !ok {
			return errors.New("invalid Redis vote status")
		}
		if status < 0 {
			return errors.New("Redis count hash is missing the selected option")
		}
		if status == 0 {
			return nil
		}
		if len(items) != len(optionIDs)+2 {
			return errors.New("incomplete Redis vote counts")
		}
		result.Total, ok = items[1].(int64)
		if !ok {
			return errors.New("invalid Redis vote total")
		}
		for i, id := range optionIDs {
			count, ok := items[i+2].(int64)
			if !ok {
				return errors.New("invalid Redis option count")
			}
			result.Counts[id] = count
		}
		return nil
	})
	if err != nil {
		return VoteResult{}, false, err
	}
	return result, result.Total > 0, nil
}

func (c *Client) EnsureVoteStreamGroup(ctx context.Context, group string) error {
	return withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		_, err := commandValue(conn, reader, "XGROUP", "CREATE", voteStreamKey, group, "0", "MKSTREAM")
		if err != nil && strings.Contains(err.Error(), "BUSYGROUP") {
			return nil
		}
		return err
	})
}

func (c *Client) ReadVoteStream(ctx context.Context, group, consumer string, count int) ([]StreamMessage, error) {
	var messages []StreamMessage
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "XREADGROUP", "GROUP", group, consumer, "COUNT", strconv.Itoa(count), "STREAMS", voteStreamKey, ">")
		if err != nil {
			return err
		}
		messages = parseStreamMessages(response)
		return nil
	})
	return messages, err
}

func (c *Client) ClaimVoteStream(ctx context.Context, group, consumer string, idle time.Duration, count int) ([]StreamMessage, error) {
	var messages []StreamMessage
	err := withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		response, err := commandValue(conn, reader, "XAUTOCLAIM", voteStreamKey, group, consumer, strconv.FormatInt(max(int64(1), idle.Milliseconds()), 10), "0-0", "COUNT", strconv.Itoa(count))
		if err != nil {
			return err
		}
		items, ok := response.([]any)
		if !ok || len(items) < 2 {
			return errors.New("unexpected Redis XAUTOCLAIM response")
		}
		messages = parseStreamMessages(items[1])
		return nil
	})
	return messages, err
}

func (c *Client) AckVoteStream(ctx context.Context, group, id string) error {
	return withConnection(ctx, c.url, func(conn net.Conn, reader *bufio.Reader) error {
		_, err := commandValue(conn, reader, "XACK", voteStreamKey, group, id)
		return err
	})
}

func parseStreamMessages(response any) []StreamMessage {
	streams, ok := response.([]any)
	if !ok {
		return nil
	}
	var result []StreamMessage
	for _, rawStream := range streams {
		stream, ok := rawStream.([]any)
		if !ok || len(stream) != 2 {
			continue
		}
		entries, ok := stream[1].([]any)
		if !ok {
			continue
		}
		for _, rawEntry := range entries {
			entry, ok := rawEntry.([]any)
			if !ok || len(entry) != 2 {
				continue
			}
			id, ok := entry[0].(string)
			if !ok {
				continue
			}
			fieldsRaw, ok := entry[1].([]any)
			if !ok || len(fieldsRaw)%2 != 0 {
				continue
			}
			fields := make(map[string]string, len(fieldsRaw)/2)
			for i := 0; i < len(fieldsRaw); i += 2 {
				key, keyOK := fieldsRaw[i].(string)
				value, valueOK := fieldsRaw[i+1].(string)
				if keyOK && valueOK {
					fields[key] = value
				}
			}
			result = append(result, StreamMessage{ID: id, Fields: fields})
		}
	}
	return result
}

func ttlSeconds(ttl time.Duration) int64 {
	seconds := int64(ttl / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func isOKResponse(response any) bool {
	value, ok := response.(string)
	return ok && value == "+OK"
}

func withConnection(ctx context.Context, rawURL string, run func(net.Conn, *bufio.Reader) error) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "redis" && u.Scheme != "rediss") {
		return errors.New("REDIS_URL must be a " + "redis" + ":// or " + "rediss" + ":// URL")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	var conn net.Conn
	if u.Scheme == "rediss" {
		conn, err = tls.DialWithDialer(dialer, "tcp", u.Host, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", u.Host)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	if err := authenticate(conn, reader, u); err != nil {
		return err
	}
	database := strings.TrimPrefix(u.EscapedPath(), "/")
	if database != "" && database != "0" {
		if _, err := strconv.Atoi(database); err != nil {
			return errors.New("Redis database path must be a number")
		}
		response, err := command(conn, reader, "SELECT", database)
		if err != nil {
			return err
		}
		if response != "+OK" {
			return errors.New("Redis database selection failed")
		}
	}
	return run(conn, reader)
}

func authenticate(conn net.Conn, reader *bufio.Reader, u *url.URL) error {
	if u.User == nil {
		return nil
	}
	password, hasPassword := u.User.Password()
	username := u.User.Username()
	if !hasPassword && username == "" {
		return nil
	}
	args := []string{"AUTH"}
	if username != "" && username != "default" {
		args = append(args, username)
	}
	if hasPassword {
		args = append(args, password)
	}
	response, err := command(conn, reader, args...)
	if err != nil {
		return err
	}
	if response != "+OK" {
		return errors.New("Redis authentication failed")
	}
	return nil
}

func command(conn net.Conn, reader *bufio.Reader, args ...string) (string, error) {
	value, err := commandValue(conn, reader, args...)
	if err != nil {
		return "", err
	}
	switch response := value.(type) {
	case string:
		if strings.HasPrefix(response, "!") {
			return "", errors.New(strings.TrimPrefix(response, "!"))
		}
		return response, nil
	case int64:
		return ":" + strconv.FormatInt(response, 10), nil
	default:
		return "", errors.New("unexpected Redis response type")
	}
}

func commandValue(conn net.Conn, reader *bufio.Reader, args ...string) (any, error) {
	if _, err := fmt.Fprintf(conn, "*%d\r\n", len(args)); err != nil {
		return nil, err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(conn, "$%d\r\n%s\r\n", len([]byte(arg)), arg); err != nil {
			return nil, err
		}
	}
	return readResponse(reader)
}

func readResponse(reader *bufio.Reader) (any, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	switch prefix {
	case '+':
		return "+" + line, nil
	case '-':
		return nil, errors.New(line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		length, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, nil
		}
		value := make([]byte, length+2)
		if _, err := io.ReadFull(reader, value); err != nil {
			return nil, err
		}
		return string(value[:length]), nil
	case '*':
		length, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, nil
		}
		items := make([]any, length)
		for i := range items {
			items[i], err = readResponse(reader)
			if err != nil {
				return nil, err
			}
		}
		return items, nil
	default:
		return nil, errors.New("unsupported Redis response")
	}
}
