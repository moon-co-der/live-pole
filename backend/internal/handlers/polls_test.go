package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"live-polling/internal/middleware"
	"live-polling/internal/models"
	"live-polling/internal/redisx"
	"live-polling/internal/repository"
	"live-polling/internal/services"
)

type memoryPolls struct {
	items     map[string]*models.Poll
	next      byte
	votes     *memoryVotes
	findError error
}

func newMemoryPolls() *memoryPolls                         { return &memoryPolls{items: map[string]*models.Poll{}} }
func (m *memoryPolls) EnsureIndexes(context.Context) error { return nil }
func (m *memoryPolls) Create(_ context.Context, poll *models.Poll) error {
	for _, existing := range m.items {
		if existing.Slug == poll.Slug {
			return mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000}}}
		}
	}
	m.next++
	poll.ID = bson.ObjectID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, m.next}
	m.items[poll.ID.Hex()] = poll
	return nil
}
func (m *memoryPolls) DeleteByID(_ context.Context, id bson.ObjectID) error {
	delete(m.items, id.Hex())
	return nil
}
func (m *memoryPolls) FindOwned(_ context.Context, id, owner bson.ObjectID) (*models.Poll, error) {
	poll := m.items[id.Hex()]
	if poll == nil || poll.Owner != owner {
		return nil, nil
	}
	return poll, nil
}
func (m *memoryPolls) FindByID(_ context.Context, id bson.ObjectID) (*models.Poll, error) {
	return m.items[id.Hex()], nil
}
func (m *memoryPolls) FindBySlug(_ context.Context, slug string) (*models.Poll, error) {
	if m.findError != nil {
		return nil, m.findError
	}
	for _, poll := range m.items {
		if poll.Slug == slug {
			return poll, nil
		}
	}
	return nil, nil
}
func (m *memoryPolls) IncrementViews(_ context.Context, id bson.ObjectID) error {
	if poll := m.items[id.Hex()]; poll != nil {
		poll.Views++
	}
	return nil
}
func (m *memoryPolls) IncrementVoteCount(_ context.Context, id bson.ObjectID, optionID string) error {
	if poll := m.items[id.Hex()]; poll != nil {
		poll.TotalVotes++
		for i := range poll.Options {
			if poll.Options[i].ID == optionID {
				poll.Options[i].Votes++
				return nil
			}
		}
	}
	return nil
}
func (m *memoryPolls) CloseOwned(_ context.Context, id, owner bson.ObjectID) error {
	if poll := m.items[id.Hex()]; poll != nil && poll.Owner == owner {
		poll.Status = models.PollCompleted
		return nil
	}
	return nil
}
func (m *memoryPolls) ActivePolls(context.Context) ([]models.Poll, error) {
	items := make([]models.Poll, 0)
	for _, poll := range m.items {
		if poll.Status == models.PollActive {
			items = append(items, *poll)
		}
	}
	return items, nil
}
func (m *memoryPolls) SyncVoteCounts(_ context.Context, id bson.ObjectID, options []models.PollOption, total int64) error {
	if poll := m.items[id.Hex()]; poll != nil {
		poll.Options = options
		poll.TotalVotes = total
	}
	return nil
}
func (m *memoryPolls) DeleteOwned(_ context.Context, id, owner bson.ObjectID) error {
	if p := m.items[id.Hex()]; p != nil && p.Owner == owner {
		delete(m.items, id.Hex())
	}
	return nil
}
func (m *memoryPolls) FindExpired(_ context.Context, now time.Time) ([]models.Poll, error) {
	items := make([]models.Poll, 0)
	for _, p := range m.items {
		if p.Status == models.PollActive && p.EndsAt != nil && !p.EndsAt.After(now) {
			items = append(items, *p)
		}
	}
	return items, nil
}
func (m *memoryPolls) CompleteExpired(_ context.Context, id bson.ObjectID, now time.Time) (bool, error) {
	p := m.items[id.Hex()]
	if p == nil || p.Status != models.PollActive || p.EndsAt == nil || p.EndsAt.After(now) {
		return false, nil
	}
	p.Status = models.PollCompleted
	p.UpdatedAt = now
	return true, nil
}
func (m *memoryPolls) List(_ context.Context, filter repository.PollListFilter) ([]models.Poll, int64, error) {
	items := make([]models.Poll, 0)
	for _, poll := range m.items {
		if poll.Owner != filter.Owner || (filter.Status != "" && string(poll.Status) != filter.Status) ||
			(filter.Search != "" && !strings.Contains(strings.ToLower(poll.Question), strings.ToLower(filter.Search))) {
			continue
		}
		items = append(items, *poll)
	}
	if filter.Sort == "votes" {
		sort.Slice(items, func(i, j int) bool { return items[i].TotalVotes > items[j].TotalVotes })
	}
	total := int64(len(items))
	if filter.Skip >= total {
		return []models.Poll{}, total, nil
	}
	start, end := int(filter.Skip), int(filter.Skip+filter.Limit)
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

type memoryCounts struct {
	hashes    map[string]map[string]string
	locks     map[string]bool
	ttls      map[string]time.Duration
	stream    []redisx.VoteStreamEntry
	metadata  map[string]string
	published []struct{ channel, payload string }
	mu        sync.Mutex
}

func newMemoryCounts() *memoryCounts {
	return &memoryCounts{hashes: map[string]map[string]string{}, locks: map[string]bool{}, ttls: map[string]time.Duration{}, metadata: map[string]string{}}
}
func (m *memoryCounts) HSet(_ context.Context, key string, fields map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hashes[key] = fields
	return nil
}
func (m *memoryCounts) HSetTTL(ctx context.Context, key string, fields map[string]string, ttl time.Duration) error {
	if err := m.HSet(ctx, key, fields); err != nil {
		return err
	}
	m.mu.Lock()
	m.ttls[key] = ttl
	m.mu.Unlock()
	return nil
}
func (m *memoryCounts) Publish(_ context.Context, channel, payload string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, struct{ channel, payload string }{channel, payload})
	return nil
}
func (m *memoryCounts) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metadata[key] = value
	return nil
}
func (m *memoryCounts) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, found := m.metadata[key]
	return value, found, nil
}
func (m *memoryCounts) GetDel(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, found := m.metadata[key]
	delete(m.metadata, key)
	return value, found, nil
}
func (m *memoryCounts) DeletePollKeys(_ context.Context, pollID, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.hashes, "poll:"+pollID+":counts")
	delete(m.metadata, "poll:slug:"+slug)
	for key := range m.locks {
		if strings.Contains(key, ":"+pollID+":") || strings.Contains(key, ":"+pollID) {
			delete(m.locks, key)
		}
	}
	return nil
}
func (m *memoryCounts) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, hashExists := m.hashes[key]
	return m.locks[key] || hashExists, nil
}
func (m *memoryCounts) HGetAll(_ context.Context, key string) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]int64{}
	for k, v := range m.hashes[key] {
		n, _ := strconv.ParseInt(v, 10, 64)
		result[k] = n
	}
	return result, nil
}
func (m *memoryCounts) TTL(_ context.Context, key string) (time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.hashes[key]; !exists {
		return -2 * time.Second, nil
	}
	if ttl := m.ttls[key]; ttl > 0 {
		return ttl, nil
	}
	return -time.Second, nil
}
func (m *memoryCounts) SetNX(_ context.Context, key string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks[key] {
		return false, nil
	}
	m.locks[key] = true
	m.ttls[key] = ttl
	return true, nil
}
func (m *memoryCounts) CastVoteWithTTL(_ context.Context, voterKey, countsKey string, optionID string, optionIDs []string, ttl time.Duration, entry redisx.VoteStreamEntry) (redisx.VoteResult, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks[voterKey] {
		return redisx.VoteResult{}, false, nil
	}
	if _, ok := m.hashes[countsKey][optionID]; !ok {
		return redisx.VoteResult{}, false, errors.New("unknown option")
	}
	m.locks[voterKey] = true
	m.stream = append(m.stream, entry)
	m.ttls[voterKey] = ttl
	m.ttls[countsKey] = ttl
	count, _ := strconv.ParseInt(m.hashes[countsKey][optionID], 10, 64)
	count++
	m.hashes[countsKey][optionID] = strconv.FormatInt(count, 10)
	total, _ := strconv.ParseInt(m.hashes[countsKey]["total"], 10, 64)
	total++
	m.hashes[countsKey]["total"] = strconv.FormatInt(total, 10)
	counts := map[string]int64{}
	for _, id := range optionIDs {
		n, _ := strconv.ParseInt(m.hashes[countsKey][id], 10, 64)
		counts[id] = n
	}
	return redisx.VoteResult{Counts: counts, Total: total}, true, nil
}

type memoryVotes struct{ items []*models.Vote }

type memoryViews struct{ items []*models.View }

func (m *memoryViews) EnsureIndexes(context.Context) error { return nil }
func (m *memoryViews) Create(_ context.Context, v *models.View) error {
	m.items = append(m.items, v)
	return nil
}
func (m *memoryViews) ByPolls(_ context.Context, ids []bson.ObjectID) ([]models.View, error) {
	out := make([]models.View, 0)
	for _, v := range m.items {
		for _, id := range ids {
			if v.PollID == id {
				out = append(out, *v)
				break
			}
		}
	}
	return out, nil
}
func (m *memoryViews) DeleteByPoll(_ context.Context, pollID bson.ObjectID) error {
	filtered := m.items[:0]
	for _, view := range m.items {
		if view.PollID != pollID {
			filtered = append(filtered, view)
		}
	}
	m.items = filtered
	return nil
}

func (m *memoryVotes) EnsureIndexes(context.Context) error { return nil }
func (m *memoryVotes) Create(_ context.Context, vote *models.Vote) error {
	for _, existing := range m.items {
		if existing.VoteID == vote.VoteID || (existing.PollID == vote.PollID && existing.VoterHash == vote.VoterHash) {
			return mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000}}}
		}
	}
	m.items = append(m.items, vote)
	return nil
}
func (m *memoryVotes) CountsByPoll(_ context.Context, pollID bson.ObjectID) (map[string]int64, error) {
	counts := map[string]int64{"total": 0}
	for _, vote := range m.items {
		if vote.PollID == pollID {
			counts[vote.OptionID]++
			counts["total"]++
		}
	}
	return counts, nil
}
func (m *memoryVotes) DeleteByPoll(_ context.Context, pollID bson.ObjectID) error {
	filtered := m.items[:0]
	for _, v := range m.items {
		if v.PollID != pollID {
			filtered = append(filtered, v)
		}
	}
	m.items = filtered
	return nil
}
func (m *memoryVotes) VotesByPoll(_ context.Context, pollID bson.ObjectID) ([]models.Vote, error) {
	items := make([]models.Vote, 0)
	for _, v := range m.items {
		if v.PollID == pollID {
			items = append(items, *v)
		}
	}
	return items, nil
}

func pollTestServer() (*gin.Engine, *memoryPolls, *memoryCounts, string) {
	gin.SetMode(gin.TestMode)
	secret := "poll-http-test-secret-that-has-at-least-32-chars"
	store, counts := newMemoryPolls(), newMemoryCounts()
	store.votes = &memoryVotes{}
	views := &memoryViews{}
	service := services.NewPollService(repository.PollStore(store), repository.VoteStore(store.votes), views, counts, "redis"+"://redis.example.invalid:6379/0")
	publicService := services.NewPublicPollService(repository.PollStore(store), counts, store.votes, views, counts)
	handler := NewPollHandler(service)
	publicHandler := NewPublicPollHandler(publicService, false)
	router := gin.New()
	router.Use(middleware.JSONRecovery())
	owner := router.Group("/api", middleware.AuthRequired(secret))
	owner.POST("/polls", handler.Create)
	owner.GET("/polls", handler.List)
	owner.GET("/polls/:id", handler.Get)
	owner.PATCH("/polls/:id/close", handler.Close)
	owner.DELETE("/polls/:id", handler.Delete)
	router.GET("/api/public/polls/:slug", publicHandler.Get)
	router.POST("/api/public/polls/:slug/vote", publicHandler.Vote)
	return router, store, counts, secret
}

func TestCreatePollRequiresTokenAndCreatesRedisCounts(t *testing.T) {
	router, store, counts, secret := pollTestServer()
	body := `{"question":"  Favorite drink?  ","options":[" Tea ","Coffee"],"allowMultiple":true}`
	response := request(router, http.MethodPost, "/api/polls", body, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d", response.Code)
	}
	token, err := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	if err != nil {
		t.Fatal(err)
	}
	response = request(router, http.MethodPost, "/api/polls", body, token)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", response.Code, response.Body.String())
	}
	var result struct {
		ID        string      `json:"id"`
		ShareLink string      `json:"shareLink"`
		Poll      models.Poll `json:"poll"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || result.ShareLink != "/p/"+result.Poll.Slug || len(result.Poll.Slug) != 8 {
		t.Fatalf("invalid share response: %+v", result)
	}
	if result.Poll.Question != "Favorite drink?" || result.Poll.Options[0].Text != "Tea" || result.Poll.Options[0].Votes != 0 || !result.Poll.AllowMultiple {
		t.Fatalf("poll values were not normalized/initialized: %+v", result.Poll)
	}
	if len(store.items) != 1 {
		t.Fatalf("stored poll count = %d", len(store.items))
	}
	hash := counts.hashes["poll:"+result.ID+":counts"]
	if len(hash) != 3 || hash["option-1"] != "0" || hash["option-2"] != "0" || hash["total"] != "0" {
		t.Fatalf("unexpected Redis counts hash: %#v", hash)
	}
}

func TestCreatePollRejectsInvalidInput(t *testing.T) {
	router, store, _, secret := pollTestServer()
	token, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	invalid := []string{
		`{"question":"","options":["A","B"]}`,
		`{"question":"Valid question","options":["Only one"]}`,
		`{"question":"Valid question","options":["A","B","C","D","E","F","G"]}`,
		`{"question":"Valid question","options":["Yes"," yes "]}`,
		`{"question":"Valid question","options":[" ","B"]}`,
		`{"question":"Valid question","options":["A","B"],"endsAt":"2000-01-01T00:00:00Z"}`,
	}
	for _, body := range invalid {
		response := request(router, http.MethodPost, "/api/polls", body, token)
		if response.Code != http.StatusBadRequest {
			t.Errorf("invalid input status = %d for %s", response.Code, body)
		}
	}
	if len(store.items) != 0 {
		t.Fatalf("invalid requests persisted %d polls", len(store.items))
	}
}

func TestPollListAndDetailAreOwnerScoped(t *testing.T) {
	router, _, _, secret := pollTestServer()
	ownerA, ownerB := bson.ObjectID{1, 1, 1}, bson.ObjectID{2, 2, 2}
	tokenA, _ := services.SignToken(ownerA.Hex(), secret)
	tokenB, _ := services.SignToken(ownerB.Hex(), secret)
	create := func(token, question string) string {
		body := `{"question":"` + question + `","options":["A","B"]}`
		response := request(router, http.MethodPost, "/api/polls", body, token)
		if response.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
		}
		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.ID
	}
	pollA1 := create(tokenA, "Owner A first poll")
	create(tokenA, "Owner A second poll")
	create(tokenB, "Owner B poll")
	listA := request(router, http.MethodGet, "/api/polls?status=active&q=Owner%20A&sort=newest&page=1&limit=10", "", tokenA)
	listB := request(router, http.MethodGet, "/api/polls", "", tokenB)
	var a, b struct {
		Polls []models.Poll `json:"polls"`
	}
	if listA.Code != http.StatusOK || json.Unmarshal(listA.Body.Bytes(), &a) != nil || len(a.Polls) != 2 {
		t.Fatalf("owner A list: %d %s", listA.Code, listA.Body.String())
	}
	if listB.Code != http.StatusOK || json.Unmarshal(listB.Body.Bytes(), &b) != nil || len(b.Polls) != 1 {
		t.Fatalf("owner B list: %d %s", listB.Code, listB.Body.String())
	}
	if own := request(router, http.MethodGet, "/api/polls/"+pollA1, "", tokenA); own.Code != http.StatusOK {
		t.Fatalf("owner detail status=%d", own.Code)
	}
	if other := request(router, http.MethodGet, "/api/polls/"+pollA1, "", tokenB); other.Code != http.StatusNotFound {
		t.Fatalf("cross-owner detail status=%d", other.Code)
	}
}

func TestOwnerCanClosePollAndOtherOwnerCannot(t *testing.T) {
	router, store, _, secret := pollTestServer()
	tokenA, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	tokenB, _ := services.SignToken(bson.ObjectID{2, 2, 2}.Hex(), secret)
	created := request(router, http.MethodPost, "/api/polls", `{"question":"Close this poll?","options":["Yes","No"]}`, tokenA)
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	path := "/api/polls/" + response.ID + "/close"
	if wrong := request(router, http.MethodPatch, path, "", tokenB); wrong.Code != http.StatusNotFound {
		t.Fatalf("other owner's close status=%d", wrong.Code)
	}
	if closed := request(router, http.MethodPatch, path, "", tokenA); closed.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", closed.Code, closed.Body.String())
	}
	if store.items[response.ID].Status != models.PollCompleted {
		t.Fatalf("poll status=%s", store.items[response.ID].Status)
	}
}

func TestOwnerCanDeletePollAndOtherOwnerCannot(t *testing.T) {
	router, store, counts, secret := pollTestServer()
	tokenA, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	tokenB, _ := services.SignToken(bson.ObjectID{2, 2, 2}.Hex(), secret)
	created := request(router, http.MethodPost, "/api/polls", `{"question":"Delete this poll?","options":["Yes","No"]}`, tokenA)
	var response struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	poll := store.items[response.ID]
	store.votes.items = append(store.votes.items, &models.Vote{PollID: poll.ID, OptionID: poll.Options[0].ID, VoteID: "seed-vote"})
	path := "/api/polls/" + response.ID
	if wrong := request(router, http.MethodDelete, path, "", tokenB); wrong.Code != http.StatusNotFound {
		t.Fatalf("other owner's delete status=%d", wrong.Code)
	}
	if deleted := request(router, http.MethodDelete, path, "", tokenA); deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, exists := store.items[response.ID]; exists {
		t.Fatal("poll still exists")
	}
	if len(store.votes.items) != 0 {
		t.Fatal("poll votes still exist")
	}
	if _, exists := counts.hashes["poll:"+response.ID+":counts"]; exists {
		t.Fatal("poll counts hash still exists")
	}
	if len(counts.published) == 0 {
		t.Fatal("poll_closed was not published")
	}
}

func TestPublicPollCookieVoteAndReloadResults(t *testing.T) {
	router, _, counts, secret := pollTestServer()
	token, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	create := request(router, http.MethodPost, "/api/polls", `{"question":"Choose a study spot?","options":["Library","Cafe"]}`, token)
	var created struct {
		ID   string      `json:"id"`
		Poll models.Poll `json:"poll"`
	}
	if create.Code != http.StatusCreated || json.Unmarshal(create.Body.Bytes(), &created) != nil {
		t.Fatalf("create poll: %d %s", create.Code, create.Body.String())
	}
	path := "/api/public/polls/" + created.Poll.Slug
	first := publicRequest(router, http.MethodGet, path, "", "")
	if first.Code != http.StatusOK {
		t.Fatalf("public get = %d %s", first.Code, first.Body.String())
	}
	cookie := first.Result().Cookies()[0]
	if cookie.Name != voterCookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe voter cookie: %+v", cookie)
	}
	if strings.Contains(first.Body.String(), "owner") || strings.Contains(first.Body.String(), "votes") {
		t.Fatalf("public response leaks owner or counts: %s", first.Body.String())
	}
	var public struct {
		Poll     models.PublicPoll `json:"poll"`
		HasVoted bool              `json:"hasVoted"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &public); err != nil || public.Poll.Question != "Choose a study spot?" || len(public.Poll.Options) != 2 || public.HasVoted {
		t.Fatalf("bad public poll response: %+v (%v)", public, err)
	}
	if unknown := publicRequest(router, http.MethodGet, "/api/public/polls/missing", "", ""); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown poll status=%d", unknown.Code)
	}
	if empty := publicRequest(router, http.MethodPost, path+"/vote", "", cookie.Value); empty.Code != http.StatusBadRequest {
		t.Fatalf("empty vote status=%d", empty.Code)
	}
	if invalid := publicRequest(router, http.MethodPost, path+"/vote", `{"optionId":"no-such-option"}`, cookie.Value); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid option status=%d", invalid.Code)
	}
	vote := publicRequest(router, http.MethodPost, path+"/vote", `{"optionId":"option-1"}`, cookie.Value)
	if vote.Code != http.StatusOK || !strings.Contains(vote.Body.String(), `"totalVotes":1`) {
		t.Fatalf("vote status=%d body=%s", vote.Code, vote.Body.String())
	}
	if again := publicRequest(router, http.MethodPost, path+"/vote", `{"optionId":"option-2"}`, cookie.Value); again.Code != http.StatusConflict {
		t.Fatalf("second vote status=%d body=%s", again.Code, again.Body.String())
	}
	reload := publicRequest(router, http.MethodGet, path, "", cookie.Value)
	if reload.Code != http.StatusOK || !strings.Contains(reload.Body.String(), `"hasVoted":true`) || !strings.Contains(reload.Body.String(), `"totalVotes":1`) {
		t.Fatalf("reload does not return results: %d %s", reload.Code, reload.Body.String())
	}
	if got := counts.hashes["poll:"+created.ID+":counts"]; got["option-1"] != "1" || got["total"] != "1" {
		t.Fatalf("vote count not atomic: %#v", got)
	}
	if len(counts.stream) != 1 || counts.stream[0].VoterHash == cookie.Value || counts.stream[0].Device != "mobile" || counts.stream[0].VoteID == "" {
		t.Fatalf("vote was not queued with a unique id and hashed voter/device: %+v", counts.stream)
	}
}

func TestMissingCountsRebuildFromVoteRecordsAndReconcile(t *testing.T) {
	router, store, counts, secret := pollTestServer()
	token, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	created := request(router, http.MethodPost, "/api/polls", `{"question":"Which drink?","options":["Tea","Coffee"]}`, token)
	var result struct {
		ID   string      `json:"id"`
		Poll models.Poll `json:"poll"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cookie := testVoterID('R')
	path := "/api/public/polls/" + result.Poll.Slug
	vote := publicRequest(router, http.MethodPost, path+"/vote", `{"optionId":"option-2"}`, cookie)
	if vote.Code != http.StatusOK {
		t.Fatalf("vote status=%d: %s", vote.Code, vote.Body.String())
	}
	entry := counts.stream[0]
	pollID, _ := bson.ObjectIDFromHex(entry.PollID)
	createdAt, _ := time.Parse(time.RFC3339Nano, entry.CreatedAt)
	if err := store.votes.Create(context.Background(), &models.Vote{VoteID: entry.VoteID, PollID: pollID, OptionID: entry.OptionID, CreatedAt: createdAt, Device: entry.Device, VoterHash: entry.VoterHash}); err != nil {
		t.Fatal(err)
	}
	countsKey := "poll:" + result.ID + ":counts"
	voterDigest := sha256.Sum256([]byte(cookie))
	voterKey := "voted:" + result.ID + ":" + hex.EncodeToString(voterDigest[:])
	if counts.ttls[countsKey] <= 0 || counts.ttls[voterKey] <= 0 {
		t.Fatalf("vote/count TTL missing: %#v", counts.ttls)
	}
	delete(counts.hashes, countsKey)
	loaded := publicRequest(router, http.MethodGet, path, "", testVoterID('S'))
	if loaded.Code != http.StatusOK {
		t.Fatalf("public GET status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	rebuilt := counts.hashes[countsKey]
	if rebuilt["option-1"] != "0" || rebuilt["option-2"] != "1" || rebuilt["total"] != "1" {
		t.Fatalf("counts not rebuilt from MongoDB votes: %#v", rebuilt)
	}
	_ = counts.HSet(context.Background(), countsKey, map[string]string{"option-1": "0", "option-2": "77", "total": "77"})
	fixed, err := services.ReconcilePollCounts(context.Background(), store, store.votes, counts)
	if err != nil {
		t.Fatal(err)
	}
	if fixed != 1 {
		t.Fatalf("reconciled poll count=%d, want 1", fixed)
	}
	if got := counts.hashes[countsKey]; got["option-2"] != "1" || got["total"] != "1" {
		t.Fatalf("reconcile did not repair hash: %#v", got)
	}
}

func TestVoteUsesCachedPollAndDurableStreamWhileMongoIsUnavailable(t *testing.T) {
	router, store, counts, secret := pollTestServer()
	token, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	created := request(router, http.MethodPost, "/api/polls", `{"question":"Persist this vote?","options":["Yes","No"]}`, token)
	var result struct {
		Poll models.Poll `json:"poll"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	store.findError = errors.New("simulated MongoDB outage")
	vote := publicRequest(router, http.MethodPost, "/api/public/polls/"+result.Poll.Slug+"/vote", `{"optionId":"option-2"}`, testVoterID('W'))
	if vote.Code != http.StatusOK {
		t.Fatalf("vote while MongoDB unavailable status=%d body=%s", vote.Code, vote.Body.String())
	}
	if len(counts.stream) != 1 {
		t.Fatalf("durable stream entries=%d", len(counts.stream))
	}
	store.findError = nil
	inserted, err := services.PersistVoteEvent(context.Background(), store, store.votes, counts.stream[0])
	if err != nil || !inserted {
		t.Fatalf("stream persist inserted=%v err=%v", inserted, err)
	}
	inserted, err = services.PersistVoteEvent(context.Background(), store, store.votes, counts.stream[0])
	if err != nil || inserted {
		t.Fatalf("replayed stream event inserted=%v err=%v", inserted, err)
	}
	if len(store.votes.items) != 1 || store.items[result.Poll.ID.Hex()].TotalVotes != 1 || store.items[result.Poll.ID.Hex()].Options[1].Votes != 1 {
		t.Fatalf("stream persistence was not idempotent: records=%d poll=%+v", len(store.votes.items), store.items[result.Poll.ID.Hex()])
	}
}

func TestPublicViewsCountOncePerVoter(t *testing.T) {
	router, store, _, secret := pollTestServer()
	token, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	created := request(router, http.MethodPost, "/api/polls", `{"question":"Favorite season?","options":["Summer","Winter"]}`, token)
	var result struct {
		Poll models.Poll `json:"poll"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &result)
	path := "/api/public/polls/" + result.Poll.Slug
	first := publicRequest(router, http.MethodGet, path, "", "")
	voter := first.Result().Cookies()[0].Value
	for i := 0; i < 3; i++ {
		publicRequest(router, http.MethodGet, path, "", voter)
	}
	publicRequest(router, http.MethodGet, path, "", testVoterID('B'))
	if got := store.items[result.Poll.ID.Hex()].Views; got != 2 {
		t.Fatalf("views=%d; want 2", got)
	}
}

func TestParallelVotesWithSameVoterOnlyCountOnce(t *testing.T) {
	router, _, counts, secret := pollTestServer()
	token, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	created := request(router, http.MethodPost, "/api/polls", `{"question":"Best color today?","options":["Blue","Pink"]}`, token)
	var result struct {
		ID   string      `json:"id"`
		Poll models.Poll `json:"poll"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &result)
	path := "/api/public/polls/" + result.Poll.Slug + "/vote"
	voter := testVoterID('C')
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes, conflicts := 0, 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := publicRequest(router, http.MethodPost, path, `{"optionId":"option-1"}`, voter)
			mu.Lock()
			defer mu.Unlock()
			if response.Code == http.StatusOK {
				successes++
			} else if response.Code == http.StatusConflict {
				conflicts++
			}
		}()
	}
	wg.Wait()
	if successes != 1 || conflicts != 19 {
		t.Fatalf("success=%d conflicts=%d", successes, conflicts)
	}
	if got := counts.hashes["poll:"+result.ID+":counts"]["total"]; got != "1" {
		t.Fatalf("parallel vote total=%s", got)
	}
	if len(counts.stream) != 1 {
		t.Fatalf("parallel vote stream entries=%d", len(counts.stream))
	}
}

func TestPublicDraftClosedAndExpiredPolls(t *testing.T) {
	router, store, _, secret := pollTestServer()
	token, _ := services.SignToken(bson.ObjectID{1, 1, 1}.Hex(), secret)
	created := request(router, http.MethodPost, "/api/polls", `{"question":"When is lunch?","options":["Noon","Later"]}`, token)
	var result struct {
		Poll models.Poll `json:"poll"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &result)
	poll := store.items[result.Poll.ID.Hex()]
	path := "/api/public/polls/" + poll.Slug
	poll.Status = models.PollDraft
	if got := publicRequest(router, http.MethodGet, path, "", testVoterID('D')).Code; got != http.StatusNotFound {
		t.Fatalf("draft GET status=%d", got)
	}
	poll.Status = models.PollCompleted
	if got := publicRequest(router, http.MethodPost, path+"/vote", `{"optionId":"option-1"}`, testVoterID('E')).Code; got != http.StatusBadRequest {
		t.Fatalf("closed vote status=%d", got)
	}
	poll.Status = models.PollActive
	expired := time.Now().Add(-time.Minute)
	poll.EndsAt = &expired
	if got := publicRequest(router, http.MethodPost, path+"/vote", `{"optionId":"option-1"}`, testVoterID('F')).Code; got != http.StatusBadRequest {
		t.Fatalf("expired vote status=%d", got)
	}
}

func publicRequest(router http.Handler, method, path, body, voterID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if voterID != "" {
		req.AddCookie(&http.Cookie{Name: voterCookieName, Value: voterID})
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Example Android Mobile")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func testVoterID(char byte) string {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = char
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}
