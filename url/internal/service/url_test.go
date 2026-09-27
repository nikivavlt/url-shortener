package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nikivavlt/url-shortener/url/internal/domain"
	"github.com/nikivavlt/url-shortener/url/internal/service/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var fixedNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

type deps struct {
	repo  *mocks.MockRepository
	cache *mocks.MockCache
	users *mocks.MockUserClient
	svc   *URLService
}

func newDeps(t *testing.T) deps {
	t.Helper()
	d := deps{
		repo:  mocks.NewMockRepository(t),
		cache: mocks.NewMockCache(t),
		users: mocks.NewMockUserClient(t),
	}
	d.svc = New(d.repo, d.cache, d.users, "https://short.url/")
	d.svc.now = func() time.Time { return fixedNow }
	return d
}

func codes(cs ...string) func() (string, error) {
	i := 0
	return func() (string, error) { c := cs[i]; i++; return c, nil }
}

func validInput() domain.CreateInput {
	return domain.CreateInput{OriginalURL: "https://example.com/a", UserID: "u1"}
}

func TestCreateShortURL_Success(t *testing.T) {
	d := newDeps(t)
	d.svc.newCode = codes("abc1234")
	ctx := context.Background()

	d.cache.EXPECT().GetLimit(mock.Anything, "u1").Return(0, domain.ErrCacheMiss).Once()
	d.users.EXPECT().GetLimit(mock.Anything, "u1").Return(10, nil).Once()
	d.cache.EXPECT().SetLimit(mock.Anything, "u1", int64(10), limitCacheTTL).Return(nil).Once()
	d.cache.EXPECT().GetCounter(mock.Anything, "u1").Return(0, domain.ErrCacheMiss).Once()
	d.repo.EXPECT().CountActive(mock.Anything, "u1", fixedNow).Return(2, nil).Once()
	d.cache.EXPECT().SetCounter(mock.Anything, "u1", int64(2)).Return(nil).Once()
	d.cache.EXPECT().IncrCounter(mock.Anything, "u1").Return(3, nil).Once()
	d.repo.EXPECT().Insert(mock.Anything, mock.MatchedBy(func(l *domain.Link) bool {
		return l.ShortCode == "abc1234" && l.UserID == "u1" && l.IsActive &&
			l.ExpiresAt == nil && l.CreatedAt.Equal(fixedNow) && l.UpdatedAt.Equal(fixedNow)
	})).Return(nil).Once()
	// DecrCounter has no expectation: calling it would fail the test.

	out, err := d.svc.CreateShortURL(ctx, validInput())

	require.NoError(t, err)
	assert.Equal(t, domain.CreateOutput{ShortCode: "abc1234", ShortURL: "https://short.url/abc1234"}, out)
}

func TestCreateShortURL_LimitExceeded_CounterRolledBack(t *testing.T) {
	d := newDeps(t)

	d.cache.EXPECT().GetLimit(mock.Anything, "u1").Return(3, nil).Once()
	d.cache.EXPECT().GetCounter(mock.Anything, "u1").Return(3, nil).Once()
	d.cache.EXPECT().IncrCounter(mock.Anything, "u1").Return(4, nil).Once()
	d.cache.EXPECT().DecrCounter(mock.Anything, "u1").Return(3, nil).Once()
	// No Insert expectation: Mongo must not be touched.

	_, err := d.svc.CreateShortURL(context.Background(), validInput())

	require.ErrorIs(t, err, domain.ErrLimitExceeded)
}

func TestCreateShortURL_InsertFails_CounterRolledBack(t *testing.T) {
	d := newDeps(t)
	d.svc.newCode = codes("abc1234")
	dbErr := errors.New("mongo down")

	d.cache.EXPECT().GetLimit(mock.Anything, "u1").Return(10, nil).Once()
	d.cache.EXPECT().GetCounter(mock.Anything, "u1").Return(4, nil).Once()
	d.cache.EXPECT().IncrCounter(mock.Anything, "u1").Return(5, nil).Once()
	d.repo.EXPECT().Insert(mock.Anything, mock.Anything).Return(dbErr).Once()
	d.cache.EXPECT().DecrCounter(mock.Anything, "u1").Return(4, nil).Once()

	_, err := d.svc.CreateShortURL(context.Background(), validInput())

	require.ErrorIs(t, err, dbErr)
}

func TestCreateShortURL_CompensationSurvivesCancelledContext(t *testing.T) {
	d := newDeps(t)
	d.svc.newCode = codes("abc1234")
	ctx, cancel := context.WithCancel(context.Background())

	d.cache.EXPECT().GetLimit(mock.Anything, "u1").Return(10, nil).Once()
	d.cache.EXPECT().GetCounter(mock.Anything, "u1").Return(4, nil).Once()
	d.cache.EXPECT().IncrCounter(mock.Anything, "u1").Return(5, nil).Once()
	d.repo.EXPECT().Insert(mock.Anything, mock.Anything).
		RunAndReturn(func(context.Context, *domain.Link) error { cancel(); return context.Canceled }).Once()
	d.cache.EXPECT().DecrCounter(mock.Anything, "u1").
		Run(func(c context.Context, _ string) {
			assert.NoError(t, c.Err(), "compensation ctx must be live")
		}).
		Return(4, nil).Once()

	_, err := d.svc.CreateShortURL(ctx, validInput())

	require.ErrorIs(t, err, context.Canceled)
}

func TestCreateShortURL_RetriesOnCollision(t *testing.T) {
	d := newDeps(t)
	d.svc.newCode = codes("dup0001", "dup0002", "ok00003")

	d.cache.EXPECT().GetLimit(mock.Anything, "u1").Return(10, nil).Once()
	d.cache.EXPECT().GetCounter(mock.Anything, "u1").Return(0, nil).Once()
	d.cache.EXPECT().IncrCounter(mock.Anything, "u1").Return(1, nil).Once()
	d.repo.EXPECT().Insert(mock.Anything, mock.Anything).Return(domain.ErrShortCodeTaken).Twice()
	d.repo.EXPECT().Insert(mock.Anything, mock.Anything).Return(nil).Once()

	out, err := d.svc.CreateShortURL(context.Background(), validInput())

	require.NoError(t, err)
	assert.Equal(t, "ok00003", out.ShortCode)
}

func TestCreateShortURL_UserServiceUnavailable(t *testing.T) {
	d := newDeps(t)

	d.cache.EXPECT().GetLimit(mock.Anything, "u1").Return(0, domain.ErrCacheMiss).Once()
	d.users.EXPECT().GetLimit(mock.Anything, "u1").Return(0, domain.ErrUserServiceUnavailable).Once()

	_, err := d.svc.CreateShortURL(context.Background(), validInput())

	require.ErrorIs(t, err, domain.ErrUserServiceUnavailable)
}

func TestCreateShortURL_InvalidInput(t *testing.T) {
	neg := int64(-1)
	for name, in := range map[string]domain.CreateInput{
		"no scheme":        {OriginalURL: "example.com", UserID: "u1"},
		"ftp":              {OriginalURL: "ftp://example.com", UserID: "u1"},
		"no host":          {OriginalURL: "https://", UserID: "u1"},
		"no user":          {OriginalURL: "https://example.com"},
		"negative expires": {OriginalURL: "https://example.com", UserID: "u1", ExpiresIn: &neg},
	} {
		t.Run(name, func(t *testing.T) {
			d := newDeps(t) // no expectations: nothing may be called
			_, err := d.svc.CreateShortURL(context.Background(), in)
			require.ErrorIs(t, err, domain.ErrInvalidArgument)
		})
	}
}

func TestDeleteURL_NotOwner(t *testing.T) {
	d := newDeps(t)
	d.repo.EXPECT().FindByShortCode(mock.Anything, "abc1234").
		Return(&domain.Link{ShortCode: "abc1234", UserID: "owner", IsActive: true}, nil).Once()

	err := d.svc.DeleteURL(context.Background(), "abc1234", "intruder")

	require.ErrorIs(t, err, domain.ErrPermissionDenied)
}

func TestDeleteURL_Success(t *testing.T) {
	d := newDeps(t)
	d.repo.EXPECT().FindByShortCode(mock.Anything, "abc1234").
		Return(&domain.Link{ShortCode: "abc1234", UserID: "u1", IsActive: true}, nil).Once()
	d.repo.EXPECT().DeleteByOwner(mock.Anything, "abc1234", "u1").Return(true, nil).Once()
	d.cache.EXPECT().DelLink(mock.Anything, "abc1234").Return(nil).Once()
	d.cache.EXPECT().DecrCounter(mock.Anything, "u1").Return(2, nil).Once()

	require.NoError(t, d.svc.DeleteURL(context.Background(), "abc1234", "u1"))
}

func TestRedirect_CacheMiss_WarmsCacheWithExpiryTTL(t *testing.T) {
	d := newDeps(t)
	exp := fixedNow.Add(time.Hour)

	d.cache.EXPECT().GetLink(mock.Anything, "abc1234").Return("", domain.ErrCacheMiss).Once()
	d.repo.EXPECT().FindByShortCode(mock.Anything, "abc1234").
		Return(&domain.Link{OriginalURL: "https://example.com", IsActive: true, ExpiresAt: &exp}, nil).Once()
	d.cache.EXPECT().SetLink(mock.Anything, "abc1234", "https://example.com", time.Hour).Return(nil).Once()

	got, err := d.svc.Redirect(context.Background(), "abc1234")

	require.NoError(t, err)
	assert.Equal(t, "https://example.com", got)
}

func TestRedirect_ExpiredNotCached(t *testing.T) {
	d := newDeps(t)
	exp := fixedNow.Add(-time.Second)

	d.cache.EXPECT().GetLink(mock.Anything, "abc1234").Return("", domain.ErrCacheMiss).Once()
	d.repo.EXPECT().FindByShortCode(mock.Anything, "abc1234").
		Return(&domain.Link{OriginalURL: "https://example.com", IsActive: true, ExpiresAt: &exp}, nil).Once()

	_, err := d.svc.Redirect(context.Background(), "abc1234")

	require.ErrorIs(t, err, domain.ErrLinkNotFound)
}

func TestListUserURLs_Pagination(t *testing.T) {
	d := newDeps(t)
	page := []*domain.Link{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	d.repo.EXPECT().ListByUser(mock.Anything, "u1", "", 3).Return(page, nil).Once()

	out, err := d.svc.ListUserURLs(context.Background(), "u1", 2, "")

	require.NoError(t, err)
	assert.Len(t, out.Links, 2)
	assert.Equal(t, "b", out.NextCursor)
}
