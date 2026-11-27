package domain

import (
	"errors"
	"time"
)

var (
	ErrNotFound         = errors.New("url not found")
	ErrPermissionDenied = errors.New("permission denied")
)

type ClickEvent struct {
	ShortCode string    `json:"short_code"`
	UserID    string    `json:"user_id"`
	ClickedAt time.Time `json:"clicked_at"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Referer   string    `json:"referer"`
}

type DayCount struct {
	Date  time.Time
	Count int64
}

type RefererCount struct {
	Referer string
	Count   int64
}

type Analytics struct {
	TotalClicks int64
	ByDay       []DayCount
	TopReferers []RefererCount
}
