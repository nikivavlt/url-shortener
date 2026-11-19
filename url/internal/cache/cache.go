package cache

import (
	"context"
	"encoding/json"
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

type cachedLink struct {
	OriginalURL string `json:"original_url"`
	UserID      string `json:"user_id"`
}

func (c *Cache) GetLink(ctx context.Context, shortCode string) (*domain.Link, error) {
	v, err := c.rdb.Get(ctx, linkKey(shortCode)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, domain.ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("get link: %w", err)
	}

	var cached cachedLink
	if err := json.Unmarshal([]byte(v), &cached); err != nil {
		return nil, fmt.Errorf("decode cached link: %w", err)
	}
	return &domain.Link{ShortCode: shortCode, OriginalURL: cached.OriginalURL, UserID: cached.UserID}, nil
}

func (c *Cache) SetLink(ctx context.Context, shortCode string, link *domain.Link, ttl time.Duration) error {
	data, err := json.Marshal(cachedLink{OriginalURL: link.OriginalURL, UserID: link.UserID})
	if err != nil {
		return fmt.Errorf("encode cached link: %w", err)
	}
	return c.rdb.Set(ctx, linkKey(shortCode), data, ttl).Err()
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
