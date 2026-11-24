package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	GatewayPort     string
	UserServiceAddr string
	UrlServiceAddr  string
}

func Load() (*Config, error) {
	if err := godotenv.Load("gateway/.env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	var missing []string
	get := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	cfg := &Config{
		GatewayPort:     get("GATEWAY_PORT"),
		UserServiceAddr: get("USER_SERVICE_ADDR"),
		UrlServiceAddr:  get("URL_SERVICE_ADDR"),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("required environment variables are not set: %v", missing)
	}

	return cfg, nil
}
