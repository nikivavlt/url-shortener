package service

import (
	"context"
	"fmt"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultDays = 30
	maxDays     = 365
)

type Repository interface {
	GetAnalytics(ctx context.Context, shortCode string, days int) (*domain.Analytics, error)
}

type URLClient interface {
	GetURL(ctx context.Context, request *urlv1.GetURLRequest) (*urlv1.GetURLResponse, error)
}

type AnalyticsService struct {
	repo   Repository
	client URLClient
}

func New(repo Repository, urlClient URLClient) *AnalyticsService {
	return &AnalyticsService{repo: repo, client: urlClient}
}

func (s *AnalyticsService) GetAnalytics(ctx context.Context, userID, shortCode string, days int) (*domain.Analytics, error) {
	if days <= 0 {
		days = defaultDays
	}
	if days > maxDays {
		days = maxDays
	}

	res, err := s.client.GetURL(ctx, &urlv1.GetURLRequest{ShortCode: shortCode})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, domain.ErrNotFound
		}

		return nil, fmt.Errorf("get url: %w", err)
	}

	if res.GetUserId() != userID {
		return nil, domain.ErrPermissionDenied
	}

	return s.repo.GetAnalytics(ctx, shortCode, days)
}
