package domain

import "time"

type Link struct {
	ID          string
	ShortCode   string
	OriginalURL string
	UserID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   *time.Time // nil = never expires
	IsActive    bool
}

func (l *Link) Available(now time.Time) bool {
	return l.IsActive && (l.ExpiresAt == nil || l.ExpiresAt.After(now))
}
