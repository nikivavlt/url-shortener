package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client *redis.Client
}

func NewRedis(host, port string) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr: host + ":" + port,
	})

	ctx := context.Background()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Redis{
		client: client,
	}, nil
}
