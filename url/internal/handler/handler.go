package handler

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"

	"github.com/nikivavlt/url-shortener/url/internal/domain"
)

type URLService interface {
	CreateShortURL(ctx context.Context, in domain.CreateInput) (domain.CreateOutput, error)
	GetURL(ctx context.Context, shortCode string) (*domain.Link, error)
	DeleteURL(ctx context.Context, shortCode, userID string) error
	ListUserURLs(ctx context.Context, userID string, limit int32, cursor string) (domain.ListOutput, error)
	Redirect(ctx context.Context, shortCode string) (string, error)
}

type URLHandler struct {
	urlv1.UnimplementedURLServiceServer
	svc URLService
}

func New(svc URLService) *URLHandler {
	return &URLHandler{svc: svc}
}

func (h *URLHandler) CreateShortURL(ctx context.Context, req *urlv1.CreateShortURLRequest) (*urlv1.CreateShortURLResponse, error) {
	out, err := h.svc.CreateShortURL(ctx, domain.CreateInput{
		OriginalURL: req.GetOriginalUrl(),
		UserID:      req.GetUserId(),
		ExpiresIn:   req.ExpiresIn,
	})
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &urlv1.CreateShortURLResponse{ShortCode: out.ShortCode, ShortUrl: out.ShortURL}, nil
}

func (h *URLHandler) GetURL(ctx context.Context, req *urlv1.GetURLRequest) (*urlv1.GetURLResponse, error) {
	link, err := h.svc.GetURL(ctx, req.GetShortCode())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &urlv1.GetURLResponse{
		OriginalUrl: link.OriginalURL,
		UserId:      link.UserID,
		CreatedAt:   link.CreatedAt.Unix(),
		ExpiresAt:   unixOrZero(link),
		IsActive:    link.IsActive,
	}, nil
}

func (h *URLHandler) DeleteURL(ctx context.Context, req *urlv1.DeleteURLRequest) (*urlv1.DeleteURLResponse, error) {
	if err := h.svc.DeleteURL(ctx, req.GetShortCode(), req.GetUserId()); err != nil {
		return nil, toGRPCError(err)
	}
	return &urlv1.DeleteURLResponse{}, nil
}

func (h *URLHandler) ListUserURLs(ctx context.Context, req *urlv1.ListUserURLsRequest) (*urlv1.ListUserURLsResponse, error) {
	out, err := h.svc.ListUserURLs(ctx, req.GetUserId(), req.GetLimit(), req.GetCursor())
	if err != nil {
		return nil, toGRPCError(err)
	}

	items := make([]*urlv1.URLItem, 0, len(out.Links))
	for _, l := range out.Links {
		items = append(items, &urlv1.URLItem{
			ShortCode:   l.ShortCode,
			OriginalUrl: l.OriginalURL,
			CreatedAt:   l.CreatedAt.Unix(),
			ExpiresAt:   unixOrZero(l),
			IsActive:    l.IsActive,
		})
	}
	return &urlv1.ListUserURLsResponse{Urls: items, NextCursor: out.NextCursor}, nil
}

func (h *URLHandler) Redirect(ctx context.Context, req *urlv1.RedirectRequest) (*urlv1.RedirectResponse, error) {
	originalURL, err := h.svc.Redirect(ctx, req.GetShortCode())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &urlv1.RedirectResponse{OriginalUrl: originalURL}, nil
}

func unixOrZero(l *domain.Link) int64 {
	if l.ExpiresAt == nil {
		return 0
	}
	return l.ExpiresAt.Unix()
}

func toGRPCError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrLinkNotFound), errors.Is(err, domain.ErrUserNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, domain.ErrLimitExceeded):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, domain.ErrUserServiceUnavailable):
		return status.Error(codes.Unavailable, "user service unavailable")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
