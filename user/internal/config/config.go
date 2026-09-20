package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL     string
	redisHost       string
	redisPort       string
	jwtSecret       string
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	UserServicePort string
}

func Load() (*Config, error) {
	err := godotenv.Load("../.env")
	if err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	databaseURL, err := envString("DATABASE_URL")
	if err != nil {
		return nil, err
	}

	redisHost, err := envString("REDIS_HOST")
	if err != nil {
		return nil, err
	}

	redisPort, err := envString("REDIS_PORT")
	if err != nil {
		return nil, err
	}

	jwtSecret, err := envString("JWT_SECRET")
	if err != nil {
		return nil, err
	}

	accessTokenTTL, err := envDuration("ACCESS_TOKEN_TTL")
	if err != nil {
		return nil, err
	}

	refreshTokenTTL, err := envDuration("REFRESH_TOKEN_TTL")
	if err != nil {
		return nil, err
	}

	userServicePort, err := envString("USER_SERVICE_PORT")
	if err != nil {
		return nil, err
	}

	return &Config{
		DatabaseURL:     databaseURL,
		redisHost:       redisHost,
		redisPort:       redisPort,
		jwtSecret:       jwtSecret,
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
		UserServicePort: userServicePort,
	}, nil
}

func envString(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("environment variable %q is required", key)
	}

	return value, nil
}

func envDuration(key string) (time.Duration, error) {
	value := os.Getenv(key)

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("environment variable %q has invalid duration %q: %w", key, value, err)
	}

	return parsed, nil
}
