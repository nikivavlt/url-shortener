package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	KafkaBrokers          string
	ClickhouseAddr        string
	ClickhouseDb          string
	UrlServiceAddr        string
	AnalyticsServicerPort string
}

func Load() (*Config, error) {
	if err := godotenv.Load("analytics/.env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
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
		KafkaBrokers:          get("KAFKA_BROKERS"),
		ClickhouseAddr:        get("CLICKHOUSE_ADDR"),
		ClickhouseDb:          get("CLICKHOUSE_DB"),
		UrlServiceAddr:        get("URL_SERVICE_ADDR"),
		AnalyticsServicerPort: get("ANALYTICS_SERVICE_PORT"),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("required environment variables are not set: %v", missing)
	}

	return cfg, nil
}
