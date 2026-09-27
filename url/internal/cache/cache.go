package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nikivavlt/url-shortener/url/internal/domain"
)

func linkKey(code string) string      { return "link:" + code }
func counterKey(userID string) string { return "counter:" + userID }
func limitKey(userID string) string   { return "limit:" + userID }

type Cache struct {
	rdb redis.UniversalClient
}

func New(rdb redis.UniversalClient) *Cache {
	return &Cache{rdb: rdb}
}

func (c *Cache) GetLink(ctx context.Context, shortCode string) (string, error) {
	v, err := c.rdb.Get(ctx, linkKey(shortCode)).Result()
	if errors.Is(err, redis.Nil) {
		return "", domain.ErrCacheMiss
	}
	if err != nil {
		return "", fmt.Errorf("get link: %w", err)
	}

	return v, nil
}

func (c *Cache) SetLink(ctx context.Context, shortCode string, originalURL string, ttl time.Duration) error {
	return c.rdb.Set(ctx, linkKey(shortCode), originalURL, ttl).Err()
}

func (c *Cache) DelLink(ctx context.Context, shortCode string) error {
	return c.rdb.Del(ctx, linkKey(shortCode)).Err()
}

func (c *Cache) GetLimit(ctx context.Context, userID string) (int64, error) {
	v, err := c.rdb.Get(ctx, limitKey(userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, domain.ErrCacheMiss
	}
	if err != nil {
		return 0, fmt.Errorf("get limit: %w", err)
	}

	return v, nil
}

func (c *Cache) SetLimit(ctx context.Context, userID string, limit int64, ttl time.Duration) error {
	return c.rdb.Set(ctx, limitKey(userID), limit, ttl).Err()
}

func (c *Cache) GetCounter(ctx context.Context, userID string) (int64, error) {
	v, err := c.rdb.Get(ctx, counterKey(userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, domain.ErrCacheMiss
	}
	if err != nil {
		return 0, fmt.Errorf("get counter: %w", err)
	}

	return v, nil
}

func (c *Cache) SetCounter(ctx context.Context, userID string, value int64) error {
	return c.rdb.Set(ctx, counterKey(userID), value, 0).Err()
}

func (c *Cache) IncrCounter(ctx context.Context, userID string) (int64, error) {
	return c.rdb.Incr(ctx, counterKey(userID)).Result()
}

func (c *Cache) DecrCounter(ctx context.Context, userID string) (int64, error) {
	return c.rdb.Decr(ctx, counterKey(userID)).Result()
}
