package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"

	"live-polling/internal/models"
)

type memoryUsers struct {
	byEmail map[string]*models.User
	byID    map[string]*models.User
}

func newMemoryUsers() *memoryUsers {
	return &memoryUsers{byEmail: map[string]*models.User{}, byID: map[string]*models.User{}}
}

func (m *memoryUsers) EnsureIndexes(context.Context) error { return nil }
func (m *memoryUsers) Create(_ context.Context, user *models.User) error {
	if _, exists := m.byEmail[user.Email]; exists {
		return mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000, Message: "duplicate key"}}}
	}
	user.ID = bson.ObjectID{1, 2, 3}
	m.byEmail[user.Email] = user
	m.byID[user.ID.Hex()] = user
	return nil
}
func (m *memoryUsers) FindByEmail(_ context.Context, email string) (*models.User, error) {
	user, ok := m.byEmail[email]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	return user, nil
}
func (m *memoryUsers) FindByID(_ context.Context, id bson.ObjectID) (*models.User, error) {
	user, ok := m.byID[id.Hex()]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	return user, nil
}

func TestRegisterHashesPasswordAndNormalizesEmail(t *testing.T) {
	service := NewAuthService(newMemoryUsers())
	user, err := service.Register(context.Background(), "  Test.User@Example.com ", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "test.user@example.com" {
		t.Fatalf("email was not normalized: %q", user.Email)
	}
	if user.PasswordHash == "correct-horse" || !(strings.HasPrefix(user.PasswordHash, "$2a$") || strings.HasPrefix(user.PasswordHash, "$2b$")) {
		t.Fatalf("password was not bcrypt hashed: %q", user.PasswordHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("correct-horse")); err != nil {
		t.Fatalf("hash did not match password: %v", err)
	}
}

func TestRegisterRejectsInvalidEmailAndShortPassword(t *testing.T) {
	service := NewAuthService(newMemoryUsers())
	for _, input := range []struct{ email, password string }{
		{"not-an-email", "correct-horse"},
		{"person@example.com", "12345"},
	} {
		if _, err := service.Register(context.Background(), input.email, input.password); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Register(%q) error = %v, want invalid input", input.email, err)
		}
	}
}

func TestLoginWrongPasswordAndUnknownEmailShareError(t *testing.T) {
	service := NewAuthService(newMemoryUsers())
	user, err := service.Register(context.Background(), "owner@example.com", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	_, wrongPassword := service.Login(context.Background(), user.Email, "wrong-password")
	_, unknownEmail := service.Login(context.Background(), "missing@example.com", "wrong-password")
	if !errors.Is(wrongPassword, ErrInvalidCredentials) || wrongPassword != unknownEmail {
		t.Fatalf("login errors differ: wrong=%v unknown=%v", wrongPassword, unknownEmail)
	}
}

func TestJWTClaimsAndVerification(t *testing.T) {
	secret := "test-signing-secret-that-is-at-least-32-characters"
	token, err := SignToken("user-id-123", secret)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 || claims["userId"] != "user-id-123" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	expires := time.Unix(int64(claims["exp"].(float64)), 0)
	remaining := time.Until(expires)
	if remaining < 23*time.Hour+59*time.Minute || remaining > 24*time.Hour {
		t.Fatalf("JWT expiry is not about 24 hours ahead: %s", remaining)
	}
	verified, err := VerifyToken(token, secret, time.Now())
	if err != nil || verified.UserID != "user-id-123" {
		t.Fatalf("valid token rejected: claims=%+v err=%v", verified, err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"userId":"attacker","exp":9999999999}`))
	if _, err := VerifyToken(strings.Join(parts, "."), secret, time.Now()); err == nil {
		t.Fatal("edited token payload verified")
	}
}

func TestJWTRejectsExpiredTokenAndNonHS256Algorithm(t *testing.T) {
	secret := "test-signing-secret-that-is-at-least-32-characters"
	claims, _ := json.Marshal(TokenClaims{UserID: "user-id", Expiry: time.Now().Add(-time.Second).Unix()})
	header, _ := json.Marshal(jwtHeader{Algorithm: "HS256", Type: "JWT"})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	expired := unsigned + "." + base64.RawURLEncoding.EncodeToString(sign(unsigned, secret))
	if _, err := VerifyToken(expired, secret, time.Now()); err == nil {
		t.Fatal("expired token verified")
	}
	wrongHeader, _ := json.Marshal(jwtHeader{Algorithm: "HS384", Type: "JWT"})
	unsigned = base64.RawURLEncoding.EncodeToString(wrongHeader) + "." + base64.RawURLEncoding.EncodeToString(claims)
	wrongAlgorithm := unsigned + "." + base64.RawURLEncoding.EncodeToString(sign(unsigned, secret))
	if _, err := VerifyToken(wrongAlgorithm, secret, time.Now()); err == nil {
		t.Fatal("non-HS256 token verified")
	}
}
