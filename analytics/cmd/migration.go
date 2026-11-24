package main

import (
	"context"
	"database/sql"
	"fmt"
)

func initDB(ctx context.Context, db *sql.DB, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
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
	`, tableName)

	if _, err := db.ExecContext(ctx, query); err != nil {
		db.Close()
		return fmt.Errorf("failed to create %s table: %w", tableName, err)
	}

	return nil
}
