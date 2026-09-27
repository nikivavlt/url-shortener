package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/nikivavlt/url-shortener/url/internal/domain"
)

const (
	limitCacheTTL       = 5 * time.Minute
	maxInsertAttempts   = 3
	compensationTimeout = 2 * time.Second

	defaultPageSize = 20
	maxPageSize     = 100
	maxURLLength    = 2048
)

type Repository interface {
	Insert(ctx context.Context, link *domain.Link) error
	FindByShortCode(ctx context.Context, shortCode string) (*domain.Link, error)
	DeleteByOwner(ctx context.Context, shortCode, userID string) (bool, error)
	CountActive(ctx context.Context, userID string, now time.Time) (int64, error)
	ListByUser(ctx context.Context, userID, afterID string, limit int) ([]*domain.Link, error)
}

type Cache interface {
	GetLink(ctx context.Context, shortCode string) (string, error)
	SetLink(ctx context.Context, shortCode, originalURL string, ttl time.Duration) error
	DelLink(ctx context.Context, shortCode string) error

	GetLimit(ctx context.Context, userID string) (int64, error)
	SetLimit(ctx context.Context, userID string, limit int64, ttl time.Duration) error

	GetCounter(ctx context.Context, userID string) (int64, error)
	SetCounter(ctx context.Context, userID string, value int64) error
	IncrCounter(ctx context.Context, userID string) (int64, error)
	DecrCounter(ctx context.Context, userID string) (int64, error)
}

type UserClient interface {
	GetLimit(ctx context.Context, userID string) (int64, error)
}

type URLService struct {
	repo         Repository
	cache        Cache
	users        UserClient
	shortURLBase string
	log          *slog.Logger

	// Seams for tests
	now     func() time.Time
	newCode func() (string, error)
}

func New(repo Repository, cache Cache, users UserClient, shortURLBase string) *URLService {
	return &URLService{
		repo:         repo,
		cache:        cache,
		users:        users,
		shortURLBase: strings.TrimRight(shortURLBase, "/"),
		log:          slog.Default().With("component", "url_service"),
		now:          func() time.Time { return time.Now().UTC() },
		newCode:      generateShortCode,
	}
}

func (s *URLService) CreateShortURL(ctx context.Context, in domain.CreateInput) (domain.CreateOutput, error) {
	if err := validateCreate(in); err != nil {
		return domain.CreateOutput{}, err
	}

	limit, err := s.linksLimit(ctx, in.UserID)
	if err != nil {
		return domain.CreateOutput{}, err
	}

	if err := s.ensureCounter(ctx, in.UserID); err != nil {
		return domain.CreateOutput{}, fmt.Errorf("ensure counter: %w", err)
	}

	// Reserve
	used, err := s.cache.IncrCounter(ctx, in.UserID)
	if err != nil {
		return domain.CreateOutput{}, fmt.Errorf("incr counter: %w", err)
	}
	if used > limit {
		s.compensate(ctx, in.UserID)
		return domain.CreateOutput{}, fmt.Errorf("%w: %d of %d used", domain.ErrLimitExceeded, used-1, limit)
	}

	// Persist
	now := s.now().Truncate(time.Millisecond)
	link := &domain.Link{
		OriginalURL: in.OriginalURL,
		UserID:      in.UserID,
		CreatedAt:   now,
		UpdatedAt:   now,
		IsActive:    true,
	}
	if in.ExpiresIn != nil {
		exp := now.Add(time.Duration(*in.ExpiresIn) * time.Second)
		link.ExpiresAt = &exp
	}

	if err := s.insertWithRetry(ctx, link); err != nil {
		s.compensate(ctx, in.UserID)
		return domain.CreateOutput{}, fmt.Errorf("insert link: %w", err)
	}

	return domain.CreateOutput{
		ShortCode: link.ShortCode,
		ShortURL:  s.shortURLBase + "/" + link.ShortCode,
	}, nil
}

func (s *URLService) GetURL(ctx context.Context, shortCode string) (*domain.Link, error) {
	if shortCode == "" {
		return nil, fmt.Errorf("%w: short_code is required", domain.ErrInvalidArgument)
	}

	link, err := s.repo.FindByShortCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}
	if !link.Available(s.now()) {
		return nil, domain.ErrLinkNotFound
	}
	return link, nil
}

func (s *URLService) DeleteURL(ctx context.Context, shortCode, userID string) error {
	if shortCode == "" || userID == "" {
		return fmt.Errorf("%w: short_code and user_id are required", domain.ErrInvalidArgument)
	}

	link, err := s.repo.FindByShortCode(ctx, shortCode)
	if err != nil {
		return err
	}
	if link.UserID != userID {
		return domain.ErrPermissionDenied
	}

	deleted, err := s.repo.DeleteByOwner(ctx, shortCode, userID)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if !deleted {
		return domain.ErrLinkNotFound
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), compensationTimeout)
	defer cancel()

	if err := s.cache.DelLink(cleanupCtx, shortCode); err != nil {
		s.log.Error("stale redirect cache after delete", "short_code", shortCode, "err", err)
	}
	if link.IsActive {
		if _, err := s.cache.DecrCounter(cleanupCtx, userID); err != nil {
			s.log.Error("decr counter after delete", "user_id", userID, "err", err)
		}
	}
	return nil
}

func (s *URLService) ListUserURLs(ctx context.Context, userID string, limit int32, cursor string) (domain.ListOutput, error) {
	if userID == "" {
		return domain.ListOutput{}, fmt.Errorf("%w: user_id is required", domain.ErrInvalidArgument)
	}

	pageSize := int(limit)
	switch {
	case pageSize <= 0:
		pageSize = defaultPageSize
	case pageSize > maxPageSize:
		pageSize = maxPageSize
	}

	links, err := s.repo.ListByUser(ctx, userID, cursor, pageSize+1)
	if err != nil {
		return domain.ListOutput{}, err
	}

	out := domain.ListOutput{Links: links}
	if len(links) > pageSize {
		out.Links = links[:pageSize]
		out.NextCursor = out.Links[pageSize-1].ID
	}
	return out, nil
}

func (s *URLService) Redirect(ctx context.Context, shortCode string) (string, error) {
	if shortCode == "" {
		return "", fmt.Errorf("%w: short_code is required", domain.ErrInvalidArgument)
	}

	originalURL, err := s.cache.GetLink(ctx, shortCode)
	switch {
	case err == nil:
		return originalURL, nil
	case !errors.Is(err, domain.ErrCacheMiss):
		s.log.Warn("redirect cache read failed", "short_code", shortCode, "err", err)
	}

	link, err := s.repo.FindByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}

	now := s.now()
	if !link.Available(now) {
		return "", domain.ErrLinkNotFound
	}

	var ttl time.Duration
	if link.ExpiresAt != nil {
		ttl = link.ExpiresAt.Sub(now)
	}
	if err := s.cache.SetLink(ctx, shortCode, link.OriginalURL, ttl); err != nil {
		s.log.Warn("redirect cache write failed", "short_code", shortCode, "err", err)
	}

	return link.OriginalURL, nil
}

func (s *URLService) linksLimit(ctx context.Context, userID string) (int64, error) {
	limit, err := s.cache.GetLimit(ctx, userID)
	if err == nil {
		return limit, nil
	}
	if !errors.Is(err, domain.ErrCacheMiss) {
		s.log.Warn("limit cache read failed", "user_id", userID, "err", err)
	}

	limit, err = s.users.GetLimit(ctx, userID)
	if err != nil {
		return 0, err
	}

	if err := s.cache.SetLimit(ctx, userID, limit, limitCacheTTL); err != nil {
		s.log.Warn("limit cache write failed", "user_id", userID, "err", err)
	}
	return limit, nil
}

func (s *URLService) ensureCounter(ctx context.Context, userID string) error {
	_, err := s.cache.GetCounter(ctx, userID)
	if err == nil {
		return nil
	}

	if !errors.Is(err, domain.ErrCacheMiss) {
		return err
	}

	count, err := s.repo.CountActive(ctx, userID, s.now())
	if err != nil {
		return fmt.Errorf("count active links: %w", err)
	}

	return s.cache.SetCounter(ctx, userID, count)
}

func (s *URLService) insertWithRetry(ctx context.Context, link *domain.Link) error {
	var err error
	for range maxInsertAttempts {
		if link.ShortCode, err = s.newCode(); err != nil {
			return fmt.Errorf("generate short code: %w", err)
		}
		if err = s.repo.Insert(ctx, link); !errors.Is(err, domain.ErrShortCodeTaken) {
			return err
		}
	}
	return fmt.Errorf("after %d attempts: %w", maxInsertAttempts, err)
}

func (s *URLService) compensate(ctx context.Context, userID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), compensationTimeout)
	defer cancel()

	if _, err := s.cache.DecrCounter(ctx, userID); err != nil {
		s.log.Error("counter compensation failed",
			"user_id", userID, "err", err)
	}
}

func validateCreate(in domain.CreateInput) error {
	if in.UserID == "" {
		return fmt.Errorf("%w: user_id is required", domain.ErrInvalidArgument)
	}
	if len(in.OriginalURL) > maxURLLength {
		return fmt.Errorf("%w: original_url is longer than %d", domain.ErrInvalidArgument, maxURLLength)
	}
	if !strings.HasPrefix(in.OriginalURL, "http://") && !strings.HasPrefix(in.OriginalURL, "https://") {
		return fmt.Errorf("%w: original_url must start with http:// or https://", domain.ErrInvalidArgument)
	}
	if u, err := url.Parse(in.OriginalURL); err != nil || u.Host == "" {
		return fmt.Errorf("%w: original_url is malformed", domain.ErrInvalidArgument)
	}
	if in.ExpiresIn != nil && *in.ExpiresIn <= 0 {
		return fmt.Errorf("%w: expires_in must be positive", domain.ErrInvalidArgument)
	}
	return nil
}
