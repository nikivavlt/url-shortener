package domain

import "time"

type CreateInput struct {
	OriginalURL string
	UserID      string
	ExpiresIn   *int64 // nil = never expires
}

type CreateOutput struct {
	ShortCode string
	ShortURL  string
}

type RedirectInput struct {
	ShortCode string
	IP        string
	UserAgent string
	Referer   string
}

type ListOutput struct {
	Links      []*Link
	NextCursor string
}

type BrokerMessage struct {
	ShortCode string    `json:"short_code"`
	UserID    string    `json:"user_id"`
	ClickedAt time.Time `json:"clicked_at"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Referer   string    `json:"referer"`
}
