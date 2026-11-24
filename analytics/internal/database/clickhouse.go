package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func ConnectClickHouse(ctx context.Context) (*sql.DB, error) {
	db := clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{"127.0.0.1:9000"},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
		DialTimeout: 5 * time.Second,
	})

	// Check connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("clickhouse connection failed: %w", err)
	}

	// Create table
	query := `
		CREATE TABLE IF NOT EXISTS click_events (
			short_code String,
			user_id String,
			clicked_at DateTime,
			ip String,
			user_agent String,
			referer String
		)
		ENGINE = MergeTree()
		PARTITION BY toYYYYMM(clicked_at)
		ORDER BY (short_code, clicked_at)
	`

	if _, err := db.ExecContext(ctx, query); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create click_events table: %w", err)
	}

	return db, nil
}
