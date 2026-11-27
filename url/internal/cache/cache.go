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

func linkKey(code string) string    { return "link:" + code }
func limitKey(userID string) string { return "limit:" + userID }

type Cache struct {
	rdb redis.UniversalClient
}

func New(rdb redis.UniversalClient) *Cache {
	return &Cache{rdb: rdb}
}

type cachedLink struct {
	ID          string     `json:"id"`
	ShortCode   string     `json:"short_code"`
	OriginalURL string     `json:"original_url"`
	UserID      string     `json:"user_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	IsActive    bool       `json:"is_active"`
}

func (c *Cache) GetLink(
	ctx context.Context,
	shortCode string,
) (*domain.Link, error) {
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

	return &domain.Link{
		ID:          cached.ID,
		ShortCode:   cached.ShortCode,
		OriginalURL: cached.OriginalURL,
		UserID:      cached.UserID,
		CreatedAt:   cached.CreatedAt,
		UpdatedAt:   cached.UpdatedAt,
		ExpiresAt:   cached.ExpiresAt,
		IsActive:    cached.IsActive,
	}, nil
}

func (c *Cache) SetLink(
	ctx context.Context,
	shortCode string,
	link *domain.Link,
	ttl time.Duration,
) error {
	cached := cachedLink{
		ID:          link.ID,
		ShortCode:   link.ShortCode,
		OriginalURL: link.OriginalURL,
		UserID:      link.UserID,
		CreatedAt:   link.CreatedAt,
		UpdatedAt:   link.UpdatedAt,
		ExpiresAt:   link.ExpiresAt,
		IsActive:    link.IsActive,
	}

	data, err := json.Marshal(cached)
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
