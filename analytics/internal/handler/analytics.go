package handler

import (
	"context"
	"errors"
	"log"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
	"github.com/nikivavlt/url-shortener/analytics/internal/service"
	analytics_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/analytics/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AnalyticsHandler struct {
	analytics_v1.UnimplementedAnalyticsServiceServer
	svc *service.AnalyticsService
}

func New(svc *service.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{svc: svc}
}

func (h *AnalyticsHandler) GetAnalytics(ctx context.Context, req *analytics_v1.GetAnalyticsRequest) (*analytics_v1.GetAnalyticsResponse, error) {
	if req.GetShortCode() == "" || req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "short_code and user_id are required")
	}

	analytics, err := h.svc.GetAnalytics(ctx, req.GetUserId(), req.GetShortCode(), int(req.GetDays()))
	if err != nil {
		return nil, toGRPCError(err)
	}

	resp := &analytics_v1.GetAnalyticsResponse{TotalClicks: analytics.TotalClicks}
	for _, d := range analytics.ByDay {
		resp.ClicksByDay = append(resp.ClicksByDay, &analytics_v1.DayClicks{
			Date: d.Date.Format("2006-01-02"), Clicks: d.Count,
		})
	}
	for _, r := range analytics.TopReferers {
		resp.TopReferers = append(resp.TopReferers, &analytics_v1.RefererClicks{
			Referer: r.Referer, Clicks: r.Count,
		})
	}
	return resp, nil
}

func toGRPCError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "url not found")
	case errors.Is(err, domain.ErrPermissionDenied):
		return status.Error(codes.NotFound, "you don't own this url")
	default:
		log.Printf("internal error: %v", err)
		return status.Error(codes.Internal, "internal error")
	}
}
