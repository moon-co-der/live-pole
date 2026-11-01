package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"live-polling/internal/middleware"
	"live-polling/internal/models"
	"live-polling/internal/repository"
	"live-polling/internal/services"
)

type testUserStore struct {
	byEmail map[string]*models.User
	byID    map[string]*models.User
}

func newTestUserStore() *testUserStore {
	return &testUserStore{byEmail: map[string]*models.User{}, byID: map[string]*models.User{}}
}
func (s *testUserStore) EnsureIndexes(context.Context) error { return nil }
func (s *testUserStore) Create(_ context.Context, user *models.User) error {
	if _, exists := s.byEmail[user.Email]; exists {
		return mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000}}}
	}
	user.ID = bson.ObjectID{1, 2, 3}
	s.byEmail[user.Email] = user
	s.byID[user.ID.Hex()] = user
	return nil
}
func (s *testUserStore) FindByEmail(_ context.Context, email string) (*models.User, error) {
	user, ok := s.byEmail[email]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	return user, nil
}
func (s *testUserStore) FindByID(_ context.Context, id bson.ObjectID) (*models.User, error) {
	user, ok := s.byID[id.Hex()]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	return user, nil
}

func authTestRouter() (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)
	secret := "auth-handler-test-secret-at-least-32-characters"
	store := newTestUserStore()
	auth := services.NewAuthService(repository.UserStore(store))
	handler := NewAuthHandler(auth, secret)
	router := gin.New()
	router.POST("/api/auth/register", handler.Register)
	router.POST("/api/auth/login", handler.Login)
	router.GET("/api/auth/me", middleware.AuthRequired(secret), handler.Me)
	return router, secret
}

func request(router http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestRegisterValidationDuplicateAndPasswordHashExposure(t *testing.T) {
	router, _ := authTestRouter()
	valid := `{"email":" Owner@Example.com ","password":"correct-horse"}`
	response := request(router, http.MethodPost, "/api/auth/register", valid, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "passwordHash") || strings.Contains(response.Body.String(), "correct-horse") {
		t.Fatal("registration response exposed the password or its hash field")
	}
	response = request(router, http.MethodPost, "/api/auth/register", valid, "")
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, body=%s", response.Code, response.Body.String())
	}
	for _, body := range []string{
		`{"email":"bad-email","password":"correct-horse"}`,
		`{"email":"owner@example.com","password":"short"}`,
	} {
		response = request(router, http.MethodPost, "/api/auth/register", body, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid registration status = %d, body=%s", response.Code, response.Body.String())
		}
	}
}

func TestLoginTypeValidationIdenticalFailuresAndBearerMe(t *testing.T) {
	router, secret := authTestRouter()
	response := request(router, http.MethodPost, "/api/auth/register", `{"email":"owner@example.com","password":"correct-horse"}`, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("setup registration status = %d", response.Code)
	}

	response = request(router, http.MethodPost, "/api/auth/login", `{"email":{"$ne":null},"password":"x"}`, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("non-string login body status = %d", response.Code)
	}
	wrong := request(router, http.MethodPost, "/api/auth/login", `{"email":"owner@example.com","password":"wrong-horse"}`, "")
	unknown := request(router, http.MethodPost, "/api/auth/login", `{"email":"unknown@example.com","password":"wrong-horse"}`, "")
	if wrong.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized || wrong.Body.String() != unknown.Body.String() {
		t.Fatalf("wrong and unknown login responses differ: %d %s / %d %s", wrong.Code, wrong.Body.String(), unknown.Code, unknown.Body.String())
	}

	login := request(router, http.MethodPost, "/api/auth/login", `{"email":"owner@example.com","password":"correct-horse"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", login.Code, login.Body.String())
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &result); err != nil || result.Token == "" {
		t.Fatalf("login response has no token: %v", err)
	}

	for _, token := range []string{"", "garbage", tamperPayload(result.Token), expiredToken(secret)} {
		me := request(router, http.MethodGet, "/api/auth/me", "", token)
		if me.Code != http.StatusUnauthorized {
			t.Fatalf("invalid token returned %d, body=%s", me.Code, me.Body.String())
		}
	}
	me := request(router, http.MethodGet, "/api/auth/me", "", result.Token)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), "owner@example.com") {
		t.Fatalf("valid token /me response = %d, %s", me.Code, me.Body.String())
	}
}

func TestAuthRejectsWrongTypesUnknownFieldsTrailingJSONAndOversizedBodies(t *testing.T) {
	router, _ := authTestRouter()
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "object injection", body: `{"email":{"$ne":null},"password":"correct-horse"}`, want: http.StatusBadRequest},
		{name: "wrong email type", body: `{"email":42,"password":"correct-horse"}`, want: http.StatusBadRequest},
		{name: "unexpected field", body: `{"email":"owner@example.com","password":"correct-horse","role":"admin"}`, want: http.StatusBadRequest},
		{name: "multiple JSON values", body: `{"email":"owner@example.com","password":"correct-horse"} {}`, want: http.StatusBadRequest},
		{name: "oversized body", body: `{"email":"owner@example.com","password":"` + strings.Repeat("x", maxJSONBodyBytes) + `"}`, want: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(router, http.MethodPost, "/api/auth/login", test.body, "")
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
			if !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("expected JSON error response, got %q", response.Header().Get("Content-Type"))
			}
		})
	}
}

func tamperPayload(token string) string {
	parts := strings.Split(token, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"userId":"edited-user","exp":9999999999}`))
	return strings.Join(parts, ".")
}

func expiredToken(secret string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"userId":"%s","exp":%d}`, bson.ObjectID{1, 2, 3}.Hex(), time.Now().Add(-time.Hour).Unix())))
	unsigned := header + "." + claims
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
