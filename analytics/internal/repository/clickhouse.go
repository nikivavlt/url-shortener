package repository

import "database/sql"

type AnalyticsRepository struct {
	database *sql.DB
}

func (d *AnalyticsRepository) insertBatch() {
	// batch, _ := conn.PrepareBatch(ctx, "INSERT INTO click_events (short_code, user_id, clicked_at, ip, user_agent, referer)")
	// for _, e := range events {
	// 	batch.Append(e.ShortCode, e.UserID, e.ClickedAt, e.IP, e.UserAgent, e.Referer)
	// }
	// batch.Send()
}
