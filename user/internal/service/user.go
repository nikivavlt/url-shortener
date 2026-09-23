package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrEmailExists        = errors.New("email already exists")
	ErrInvalidEmail       = errors.New("invalid email")
	ErrPasswordTooShort   = errors.New("password too short")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
)

const minPasswordLength = 8

type UserRepository interface {
	Create(ctx context.Context, email, passwordHash string) (string, error)
	FindByEmail(ctx context.Context, email string) (userID, passwordHash string, err error)
	CreateSession(ctx context.Context, userID string, expiresAt time.Time) (string, error)
	GetActiveSessionUserID(ctx context.Context, sid string) (string, error)
	RevokeSession(ctx context.Context, sid string) error
	GetSubscriptionLimit(ctx context.Context, userID string) (int32, error)
}

type Cache interface {
	SetSession(ctx context.Context, sid, userID string, ttl time.Duration) error
	GetSession(ctx context.Context, sid string) (string, error)
	DelSession(ctx context.Context, sid string) error
}

type Auth interface {
	HashPassword(password string) (string, error)
	ComparePassword(hash, password string) error
	SignAccess(userID, sid string) (string, error)
	SignRefresh(userID, sid string) (string, error)
	Parse(token string) (userID, sid string, err error)
}

type UserService struct {
	userRepository UserRepository
	cache          Cache
	auth           Auth
	accessTTL      time.Duration
	refreshTTL     time.Duration
}

func New(repo UserRepository, cache Cache, auth Auth, accessTTL, refreshTTL time.Duration) *UserService {
	return &UserService{
		userRepository: repo,
		cache:          cache,
		auth:           auth,
		accessTTL:      accessTTL,
		refreshTTL:     refreshTTL,
	}
}

func (s *UserService) Register(ctx context.Context, email, password string) (string, error) {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return "", ErrInvalidEmail
	}

	if len(password) < minPasswordLength {
		return "", ErrPasswordTooShort
	}

	hash, err := s.auth.HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	userID, err := s.userRepository.Create(ctx, email, hash)
	if err != nil {
		return "", err
	}

	return userID, nil
}

func (s *UserService) Login(ctx context.Context, email, password string) (accessToken, refreshToken string, err error) {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return "", "", ErrInvalidCredentials
	}

	if len(password) < minPasswordLength {
		return "", "", ErrInvalidCredentials
	}

	userID, hash, err := s.userRepository.FindByEmail(ctx, email)
	if err != nil {
		return "", "", err
	}

	if err := s.auth.ComparePassword(hash, password); err != nil {
		return "", "", ErrInvalidCredentials
	}

	expiresAt := time.Now().Add(s.refreshTTL)
	sid, err := s.userRepository.CreateSession(ctx, userID, expiresAt)
	if err != nil {
		return "", "", fmt.Errorf("create session: %w", err)
	}

	accessToken, err = s.auth.SignAccess(userID, sid)
	if err != nil {
		return "", "", fmt.Errorf("sign access: %w", err)
	}
	refreshToken, err = s.auth.SignRefresh(userID, sid)
	if err != nil {
		return "", "", fmt.Errorf("sign refresh: %w", err)
	}

	_ = s.cache.SetSession(ctx, sid, userID, s.accessTTL)

	return accessToken, refreshToken, nil
}

func (s *UserService) Logout(ctx context.Context, sid string) error {
	if err := s.userRepository.RevokeSession(ctx, sid); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	_ = s.cache.DelSession(ctx, sid)

	return nil
}

func (s *UserService) ValidateSession(ctx context.Context, accessToken string) (userID, sid string, err error) {
	_, sid, err = s.auth.Parse(accessToken)
	if err != nil {
		return "", "", ErrUnauthenticated
	}

	userID, err = s.getActiveSessionUserID(ctx, sid)
	if err != nil {
		return "", "", err
	}

	return userID, sid, nil
}

func (s *UserService) RefreshToken(ctx context.Context, refreshToken string) (string, error) {
	_, sid, err := s.auth.Parse(refreshToken)
	if err != nil {
		return "", ErrUnauthenticated
	}

	userID, err := s.getActiveSessionUserID(ctx, sid)
	if err != nil {
		return "", err
	}

	accessToken, err := s.auth.SignAccess(userID, sid)
	if err != nil {
		return "", fmt.Errorf("sign access: %w", err)
	}

	_ = s.cache.SetSession(ctx, sid, userID, s.accessTTL)

	return accessToken, nil
}

func (s *UserService) GetLimit(ctx context.Context, userID string) (int32, error) {
	limit, err := s.userRepository.GetSubscriptionLimit(ctx, userID)
	if err != nil {
		return 0, err
	}
	return limit, nil
}

func (s *UserService) getActiveSessionUserID(ctx context.Context, sid string) (string, error) {
	if cached, cErr := s.cache.GetSession(ctx, sid); cErr == nil && cached != "" {
		return cached, nil
	}

	userID, err := s.userRepository.GetActiveSessionUserID(ctx, sid)
	if errors.Is(err, ErrUnauthenticated) {
		return "", ErrUnauthenticated
	}
	if err != nil {
		return "", fmt.Errorf("check session: %w", err)
	}

	_ = s.cache.SetSession(ctx, sid, userID, s.accessTTL)

	return userID, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}
