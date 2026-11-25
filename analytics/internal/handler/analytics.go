package handler

import (
	"context"

	"github.com/nikivavlt/url-shortener/analytics/internal/service"
	analytics_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/analytics/v1"
)

type AnalyticsHandler struct {
	analytics_v1.UnimplementedAnalyticsServiceServer
	svc *service.AnalyticsService
}

func New(svc *service.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{svc: svc}
}

func (h *AnalyticsHandler) GetAnalytics(ctx context.Context, req *analytics_v1.GetAnalyticsRequest) (*analytics_v1.GetAnalyticsResponse, error) {
	analyticsID, err := h.svc.GetAnalytics(ctx, req.GetUserId(), req.GetShortCode(), req.GetDays())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &analytics_v1.RegisterResponse{AnalyticsId: analyticsID}, nil
}
