package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	KafkaBrokers          []string
	ClickHouseAddr        string
	ClickHouseDB          string
	ClickHouseUsername    string
	ClickHousePassword    string
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
		KafkaBrokers:          splitBrokers(get("KAFKA_BROKERS")),
		ClickHouseAddr:        get("CLICKHOUSE_ADDR"),
		ClickHouseDB:          get("CLICKHOUSE_DB"),
		ClickHouseUsername:    get("CLICKHOUSE_USERNAME"),
		ClickHousePassword:    get("CLICKHOUSE_PASSWORD"),
		UrlServiceAddr:        get("URL_SERVICE_ADDR"),
		AnalyticsServicerPort: get("ANALYTICS_SERVICE_PORT"),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("required environment variables are not set: %v", missing)
	}

	return cfg, nil
}

func splitBrokers(value string) []string {
	brokers := strings.Split(value, ",")
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}
	return brokers
}
