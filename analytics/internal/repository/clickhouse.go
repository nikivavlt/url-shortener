package repository

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
)

type AnalyticsRepository struct {
	conn driver.Conn
}

func New(conn driver.Conn) *AnalyticsRepository {
	return &AnalyticsRepository{conn: conn}
}

func (r *AnalyticsRepository) Init(ctx context.Context) error {
	q := `
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
	if err := r.conn.Exec(ctx, q); err != nil {
		return fmt.Errorf("create click_events table: %w", err)
	}
	return nil
}

func (r *AnalyticsRepository) InsertBatch(ctx context.Context, events []domain.ClickEvent) error {
	if len(events) == 0 {
		return nil
	}

	batch, err := r.conn.PrepareBatch(ctx,
		"INSERT INTO click_events (short_code, user_id, clicked_at, ip, user_agent, referer)")
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, e := range events {
		if err := batch.Append(e.ShortCode, e.UserID, e.ClickedAt, e.IP, e.UserAgent, e.Referer); err != nil {
			_ = batch.Abort()
			return fmt.Errorf("append to batch: %w", err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("send batch: %w", err)
	}

	return nil
}

func (r *AnalyticsRepository) GetAnalytics(ctx context.Context, shortCode string, days int) (*domain.Analytics, error) {
	res := &domain.Analytics{}

	if err := r.conn.QueryRow(ctx, `
	SELECT toInt64(count()) FROM click_events WHERE short_code = ? AND clicked_at >= now() - INTERVAL ? DAY`, shortCode, days,
	).Scan(&res.TotalClicks); err != nil {
		return nil, fmt.Errorf("query total: %w", err)
	}

	rows, err := r.conn.Query(ctx,
		`SELECT toDate(clicked_at) AS d, toInt64(count()) AS c
		FROM click_events
		WHERE short_code = ? AND clicked_at >= now() - INTERVAL ? DAY
		GROUP BY d ORDER BY d DESC`, shortCode, days,
	)
	if err != nil {
		return nil, fmt.Errorf("query by day: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		day := domain.DayCount{}

		if err := rows.Scan(&day.Date, &day.Count); err != nil {
			return nil, fmt.Errorf("scan by day: %w", err)
		}

		res.ByDay = append(res.ByDay, day)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate by day: %w", err)
	}

	refRows, err := r.conn.Query(ctx, `
		SELECT referer, toInt64(count()) AS c
		FROM click_events
		WHERE short_code = ? AND clicked_at >= now() - INTERVAL ? DAY
		GROUP BY referer ORDER BY c DESC LIMIT 10`, shortCode, days)
	if err != nil {
		return nil, fmt.Errorf("query referers: %w", err)
	}
	defer refRows.Close()

	for refRows.Next() {
		rc := domain.RefererCount{}
		if err := refRows.Scan(&rc.Referer, &rc.Count); err != nil {
			return nil, fmt.Errorf("scan referers: %w", err)
		}
		res.TopReferers = append(res.TopReferers, rc)
	}
	if err := refRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate referers: %w", err)
	}

	return res, nil
}
