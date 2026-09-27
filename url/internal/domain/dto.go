package domain

type CreateInput struct {
	OriginalURL string
	UserID      string
	ExpiresIn   *int64 // nil = never expires
}

type CreateOutput struct {
	ShortCode string
	ShortURL  string
}

type ListOutput struct {
	Links      []*Link
	NextCursor string
}
