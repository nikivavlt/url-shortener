package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	client *redis.Client
}

func New(client *redis.Client) *Cache {
	return &Cache{client: client}
}

func sessionKey(sid string) string {
	return "session:" + sid
}

func (r *Cache) SetSession(ctx context.Context, sid, userID string, ttl time.Duration) error {
	if err := r.client.Set(ctx, sessionKey(sid), userID, ttl).Err(); err != nil {
		return fmt.Errorf("cache set session: %w", err)
	}
	return nil
}

func (r *Cache) GetSession(ctx context.Context, sid string) (string, error) {
	userID, err := r.client.Get(ctx, sessionKey(sid)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cache get session: %w", err)
	}
	return userID, nil
}

func (r *Cache) DelSession(ctx context.Context, sid string) error {
	if err := r.client.Del(ctx, sessionKey(sid)).Err(); err != nil {
		return fmt.Errorf("cache del session: %w", err)
	}
	return nil
}
