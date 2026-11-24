package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nikivavlt/url-shortener/url/internal/domain"
	"github.com/nikivavlt/url-shortener/url/internal/repository"
)

// testEnv holds a repository wired to a throwaway database.
type testEnv struct {
	repo   *repository.URLRepository
	links  *mongo.Collection
	quotas *mongo.Collection
}

// newTestEnv connects to MONGO_TEST_URI (skips the test if unset) and creates
// a unique database that is dropped when the test finishes.
func newTestEnv(t *testing.T) testEnv {
	t.Helper()

	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI is not set; skipping Mongo integration test")
	}

	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)

	db := client.Database(fmt.Sprintf("urltest_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Drop(cctx)
		_ = client.Disconnect(cctx)
	})

	links := db.Collection("links")
	quotas := db.Collection("user_quotas")

	_, err = links.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "short_code", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	require.NoError(t, err)

	return testEnv{repo: repository.New(links, quotas), links: links, quotas: quotas}
}

// used reads the raw counter value straight from the quota collection.
func (e testEnv) used(t *testing.T, userID string) int64 {
	t.Helper()
	var doc struct {
		Used int64 `bson:"used"`
	}
	err := e.quotas.FindOne(context.Background(), bson.D{{Key: "_id", Value: userID}}).Decode(&doc)
	require.NoError(t, err)
	return doc.Used
}

func newLink(code, userID string, active bool, expiresAt *time.Time) *domain.Link {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Link{
		ShortCode:   code,
		OriginalURL: "https://example.com/" + code,
		UserID:      userID,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   expiresAt,
		IsActive:    active,
	}
}

// The test that proves the original race is closed: many parallel reservations
// against a limit must never exceed it.
func TestReserveQuota_Concurrent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	const (
		workers = 50
		limit   = 10
	)

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 0))

	var ok, exceeded atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			switch err := env.repo.ReserveQuota(ctx, "u1", limit); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, domain.ErrLimitExceeded):
				exceeded.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.EqualValues(t, limit, ok.Load())
	assert.EqualValues(t, workers-limit, exceeded.Load())
	assert.EqualValues(t, limit, env.used(t, "u1"))
}

// Reserve and release racing each other must keep used within [0, limit].
func TestReserveRelease_Concurrent_NeverExceedsLimit(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	const limit = 5

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 0))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := env.repo.ReserveQuota(ctx, "u1", limit); err == nil {
				assert.NoError(t, env.repo.ReleaseQuota(ctx, "u1"))
			} else if !errors.Is(err, domain.ErrLimitExceeded) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	// Every successful reserve was released, so the counter is back to zero.
	assert.EqualValues(t, 0, env.used(t, "u1"))
}

func TestReserveQuota_ExactlyAtLimit(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 2))

	require.NoError(t, env.repo.ReserveQuota(ctx, "u1", 3)) // 2 -> 3
	err := env.repo.ReserveQuota(ctx, "u1", 3)              // 3 >= 3
	require.ErrorIs(t, err, domain.ErrLimitExceeded)
	assert.EqualValues(t, 3, env.used(t, "u1"))
}

func TestReserveQuota_ZeroLimit(t *testing.T) {
	env := newTestEnv(t)

	err := env.repo.ReserveQuota(context.Background(), "u1", 0)

	require.ErrorIs(t, err, domain.ErrLimitExceeded)
}

func TestReserveQuota_IsolatedPerUser(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 0))
	require.NoError(t, env.repo.SeedQuota(ctx, "u2", 0))

	require.NoError(t, env.repo.ReserveQuota(ctx, "u1", 1))
	require.ErrorIs(t, env.repo.ReserveQuota(ctx, "u1", 1), domain.ErrLimitExceeded)

	require.NoError(t, env.repo.ReserveQuota(ctx, "u2", 1), "u2 must not be affected by u1")
}

func TestReleaseQuota_NeverBelowZero(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 1))

	require.NoError(t, env.repo.ReleaseQuota(ctx, "u1")) // 1 -> 0
	require.NoError(t, env.repo.ReleaseQuota(ctx, "u1")) // stays 0
	require.NoError(t, env.repo.ReleaseQuota(ctx, "u1")) // stays 0

	assert.EqualValues(t, 0, env.used(t, "u1"))
}

func TestReleaseQuota_MissingDocIsNoop(t *testing.T) {
	env := newTestEnv(t)

	require.NoError(t, env.repo.ReleaseQuota(context.Background(), "ghost"))

	n, err := env.quotas.CountDocuments(context.Background(), bson.D{{Key: "_id", Value: "ghost"}})
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "release must not create a document")
}

func TestSeedQuota_DoesNotOverwrite(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 5))
	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 99)) // must be ignored

	assert.EqualValues(t, 5, env.used(t, "u1"))
}

func TestSeedQuota_Concurrent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(v int64) {
			defer wg.Done()
			assert.NoError(t, env.repo.SeedQuota(ctx, "u1", v))
		}(int64(i))
	}
	wg.Wait()

	n, err := env.quotas.CountDocuments(ctx, bson.D{{Key: "_id", Value: "u1"}})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "exactly one document, whoever won the seed race")
}

func TestQuotaExists(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	ok, err := env.repo.QuotaExists(ctx, "u1")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 0))

	ok, err = env.repo.QuotaExists(ctx, "u1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestLowerQuotaTo_OnlyDecreases(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.repo.SeedQuota(ctx, "u1", 5))

	require.NoError(t, env.repo.LowerQuotaTo(ctx, "u1", 8)) // higher: ignored
	assert.EqualValues(t, 5, env.used(t, "u1"))

	require.NoError(t, env.repo.LowerQuotaTo(ctx, "u1", 2)) // lower: applied
	assert.EqualValues(t, 2, env.used(t, "u1"))
}

func TestInsertAndFindByShortCode(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link := newLink("abc1234", "u1", true, nil)
	require.NoError(t, env.repo.Insert(ctx, link))
	require.NotEmpty(t, link.ID, "Insert must fill the generated ID")

	got, err := env.repo.FindByShortCode(ctx, "abc1234")
	require.NoError(t, err)
	assert.Equal(t, link.ID, got.ID)
	assert.Equal(t, "https://example.com/abc1234", got.OriginalURL)
	assert.Equal(t, "u1", got.UserID)
	assert.True(t, got.IsActive)
	assert.Nil(t, got.ExpiresAt)
}

func TestInsert_DuplicateShortCode(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.repo.Insert(ctx, newLink("abc1234", "u1", true, nil)))

	err := env.repo.Insert(ctx, newLink("abc1234", "u2", true, nil))

	require.ErrorIs(t, err, domain.ErrShortCodeTaken)
}

func TestFindByShortCode_NotFound(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.repo.FindByShortCode(context.Background(), "nope123")

	require.ErrorIs(t, err, domain.ErrLinkNotFound)
}

func TestDeleteByOwner(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.repo.Insert(ctx, newLink("abc1234", "u1", true, nil)))

	deleted, err := env.repo.DeleteByOwner(ctx, "abc1234", "intruder")
	require.NoError(t, err)
	assert.False(t, deleted, "another user must not delete the link")

	deleted, err = env.repo.DeleteByOwner(ctx, "abc1234", "u1")
	require.NoError(t, err)
	assert.True(t, deleted)

	_, err = env.repo.FindByShortCode(ctx, "abc1234")
	require.ErrorIs(t, err, domain.ErrLinkNotFound)
}

// CountActive must count only active, non-expired links of the given user.
func TestCountActive(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	for _, l := range []*domain.Link{
		newLink("act0001", "u1", true, nil),     // counts: no expiry
		newLink("act0002", "u1", true, &future), // counts: not expired yet
		newLink("exp0001", "u1", true, &past),   // expired
		newLink("ina0001", "u1", false, nil),    // inactive
		newLink("oth0001", "u2", true, nil),     // another user
	} {
		require.NoError(t, env.repo.Insert(ctx, l))
	}

	n, err := env.repo.CountActive(ctx, "u1", now)

	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
}

func TestListByUser_Pagination(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	for _, code := range []string{"lst0001", "lst0002", "lst0003", "lst0004", "lst0005"} {
		require.NoError(t, env.repo.Insert(ctx, newLink(code, "u1", true, nil)))
	}
	require.NoError(t, env.repo.Insert(ctx, newLink("oth0001", "u2", true, nil)))

	page1, err := env.repo.ListByUser(ctx, "u1", "", 2)
	require.NoError(t, err)
	require.Len(t, page1, 2)

	page2, err := env.repo.ListByUser(ctx, "u1", page1[1].ID, 2)
	require.NoError(t, err)
	require.Len(t, page2, 2)

	page3, err := env.repo.ListByUser(ctx, "u1", page2[1].ID, 2)
	require.NoError(t, err)
	require.Len(t, page3, 1)

	seen := map[string]bool{}
	for _, l := range append(append(page1, page2...), page3...) {
		assert.Equal(t, "u1", l.UserID)
		assert.False(t, seen[l.ShortCode], "no duplicates across pages")
		seen[l.ShortCode] = true
	}
	assert.Len(t, seen, 5)
}

func TestListByUser_MalformedCursor(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.repo.ListByUser(context.Background(), "u1", "not-an-object-id", 10)

	require.ErrorIs(t, err, domain.ErrInvalidArgument)
}
