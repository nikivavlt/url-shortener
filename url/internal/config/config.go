package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	MongoURI        string
	MongoDB         string
	RedisAddr       string
	UserServiceAddr string
	GRPCPort        string
	ShortURLBase    string
}

func Load() (*Config, error) {
	if err := godotenv.Load("url/.env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
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
		MongoURI:        get("MONGO_URI"),
		MongoDB:         get("MONGO_DB"),
		UserServiceAddr: get("USER_SERVICE_ADDR"),
		GRPCPort:        get("URL_SERVICE_PORT"),
		ShortURLBase:    get("SHORT_URL_BASE"),
	}
	redisHost, redisPort := get("REDIS_HOST"), get("REDIS_PORT")

	if len(missing) > 0 {
		return nil, fmt.Errorf("required environment variables are not set: %v", missing)
	}

	cfg.RedisAddr = net.JoinHostPort(redisHost, redisPort)
	return cfg, nil
}
