package service

import (
	"context"
	"errors"
	"fmt"
)

var ErrUserNotFound = errors.New("user not found")

type UserRepository interface {
	Create(ctx context.Context, email, password string) (string, error)
	// FindByEmail()
	// CreateSession()
	// GetSessionByID()
	// RevokeSession()
	// GetSubscriptionLimit()
}

type Cache interface {
	// Set(ctx context.Context, key string, value string, expiration time.Duration) error
	// Get(ctx context.Context, key string) (string, error)
	// Delete(ctx context.Context, key string) error
	// SetSession(sid, userID, ttl), GetSession(sid), DelSession(sid)
}

type Auth interface {
	HashPassword(password string) (string, error)
}

type UserService struct {
	repository UserRepository
	cache      Cache
	auth       Auth
}

func NewUserService(repository UserRepository, cache Cache, auth Auth) *UserService {
	return &UserService{repository, cache, auth}
}

func (s *UserService) Register(email, password string) (string, error) {
	if password == "" { //validations

	}

	hashedPassword, err := s.auth.HashPassword(password)
	if err != nil {

	}

	userID, err := s.repository.Create(context.TODO(), email, hashedPassword)
	if err != nil {
		return "", fmt.Errorf("creating user: %w", err)
	}

	return userID, nil
}

// Доменные ошибки объявляй как var Err... = errors.New(...): их потом маппит handler.

// service UserService {
//   rpc Register (RegisterRequest) returns (RegisterResponse);
//   rpc Login (LoginRequest) returns (LoginResponse);
//   rpc Logout (LogoutRequest) returns (LogoutResponse);
//   rpc ValidateSession (ValidateSessionRequest) returns (ValidateSessionResponse);
//   rpc RefreshToken (RefreshTokenRequest) returns (RefreshTokenResponse);
//   rpc GetLimit (GetLimitRequest) returns (GetLimitResponse);
// }

// message RegisterRequest { string email = 1; string password = 2; }
// message RegisterResponse { string user_id = 1; }

// message LoginRequest { string email = 1; string password = 2; }
// message LoginResponse { string access_token = 1; string refresh_token = 2; }

// message LogoutRequest { string session_id = 1; }
// message LogoutResponse {}

// message ValidateSessionRequest { string access_token = 1; }
// message ValidateSessionResponse { string user_id = 1; string session_id = 2; }

// message RefreshTokenRequest { string refresh_token = 1; }
// message RefreshTokenResponse { string access_token = 1; }

// message GetLimitRequest { string user_id = 1; }
// message GetLimitResponse { int32 links_limit = 1; }
