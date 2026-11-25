package repository

import "database/sql"

type AnalyticsRepository struct {
	database *sql.DB
}

func New(db *sql.DB) *AnalyticsRepository {
	return &AnalyticsRepository{database: db}
}

func (r *AnalyticsRepository) InsertBatch() {
	// batch, _ := conn.PrepareBatch(ctx, "INSERT INTO click_events (short_code, user_id, clicked_at, ip, user_agent, referer)")
	// for _, e := range events {
	// 	batch.Append(e.ShortCode, e.UserID, e.ClickedAt, e.IP, e.UserAgent, e.Referer)
	// }
	// batch.Send()
}

func (r *AnalyticsRepository) GetAnalytics() {
	// SELECT count() FROM click_events WHERE short_code = ?;

	// -- by day
	// SELECT toDate(clicked_at) AS d, count() AS c
	// FROM click_events
	// WHERE short_code = ? AND clicked_at >= now() - INTERVAL ? DAY
	// GROUP BY d ORDER BY d DESC;

	// -- top referers
	// SELECT referer, count() AS c
	// FROM click_events
	// WHERE short_code = ? AND clicked_at >= now() - INTERVAL ? DAY
	// GROUP BY referer ORDER BY c DESC LIMIT 10;
}
