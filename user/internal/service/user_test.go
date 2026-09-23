package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nikivavlt/url-shortener/user/internal/service"
	"github.com/nikivavlt/url-shortener/user/internal/service/mocks"
)

const (
	testAccessTTL  = 15 * time.Minute
	testRefreshTTL = 168 * time.Hour
)

func TestUserService_Register_Success(t *testing.T) {
	repo := mocks.NewMockUserRepository(t)
	cache := mocks.NewMockCache(t)
	authn := mocks.NewMockAuth(t)

	const (
		rawEmail  = "  User@Example.COM "
		normEmail = "user@example.com"
		password  = "password123"
		hash      = "hashed-password"
		userID    = "user-123"
	)

	authn.On("HashPassword", password).Return(hash, nil).Once()
	repo.On("Create", mock.Anything, normEmail, hash).Return(userID, nil).Once()

	svc := service.New(repo, cache, authn, testAccessTTL, testRefreshTTL)

	got, err := svc.Register(context.Background(), rawEmail, password)

	require.NoError(t, err)
	require.Equal(t, userID, got)
}

func TestUserService_Register_DuplicateEmail(t *testing.T) {
	repo := mocks.NewMockUserRepository(t)
	cache := mocks.NewMockCache(t)
	authn := mocks.NewMockAuth(t)

	authn.On("HashPassword", mock.Anything).Return("hash", nil).Once()
	repo.On("Create", mock.Anything, mock.Anything, mock.Anything).
		Return("", service.ErrEmailExists).Once()

	svc := service.New(repo, cache, authn, testAccessTTL, testRefreshTTL)

	_, err := svc.Register(context.Background(), "dup@example.com", "password123")

	require.ErrorIs(t, err, service.ErrEmailExists)
}

func TestUserService_Login_WrongPassword(t *testing.T) {
	repo := mocks.NewMockUserRepository(t)
	cache := mocks.NewMockCache(t)
	authn := mocks.NewMockAuth(t)

	const (
		email    = "user@example.com"
		password = "wrong-password"
		hash     = "stored-hash"
		userID   = "user-123"
	)

	errCompare := errors.New("hash mismatch")

	repo.On("FindByEmail", mock.Anything, email).Return(userID, hash, nil).Once()
	authn.On("ComparePassword", hash, password).Return(errCompare).Once()

	svc := service.New(repo, cache, authn, testAccessTTL, testRefreshTTL)

	access, refresh, err := svc.Login(context.Background(), email, password)

	require.ErrorIs(t, err, service.ErrInvalidCredentials)
	require.Empty(t, access)
	require.Empty(t, refresh)

	repo.AssertNotCalled(t, "CreateSession", mock.Anything, mock.Anything, mock.Anything)
	authn.AssertNotCalled(t, "SignAccess", mock.Anything, mock.Anything)
	authn.AssertNotCalled(t, "SignRefresh", mock.Anything, mock.Anything)
}
