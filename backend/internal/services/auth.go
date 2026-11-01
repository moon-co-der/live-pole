package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"

	"live-polling/internal/models"
	"live-polling/internal/repository"
	"live-polling/internal/validation"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEmailExists        = errors.New("email already exists")
	ErrInvalidCredentials = errors.New("email or password is incorrect")
	ErrUserNotFound       = errors.New("user not found")
)

type AuthService struct {
	users repository.UserStore
}

func NewAuthService(users repository.UserStore) *AuthService {
	return &AuthService{users: users}
}

func (s *AuthService) Register(ctx context.Context, email, password string) (*models.User, error) {
	email, ok := validation.NormalizeEmail(email)
	if !ok || !validation.ValidPassword(password) {
		return nil, ErrInvalidInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, ErrInvalidInput
	}
	user := &models.User{Email: email, PasswordHash: string(hash), CreatedAt: time.Now().UTC()}
	if err := s.users.Create(ctx, user); err != nil {
		if repository.IsDuplicateEmail(err) {
			return nil, ErrEmailExists
		}
		return nil, err
	}
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*models.User, error) {
	email, ok := validation.NormalizeEmail(email)
	if !ok || strings.TrimSpace(password) == "" {
		return nil, ErrInvalidCredentials
	}
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

func (s *AuthService) UserByID(ctx context.Context, id string) (*models.User, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrUserNotFound
	}
	user, err := s.users.FindByID(ctx, objectID)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrUserNotFound
	}
	return user, err
}
