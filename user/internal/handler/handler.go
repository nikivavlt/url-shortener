package handler

import (
	"context"
	"errors"

	user_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
	"github.com/nikivavlt/url-shortener/user/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UserHandler struct {
	user_v1.UnimplementedUserServiceServer
	svc *service.UserService
}

func New(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) Register(ctx context.Context, req *user_v1.RegisterRequest) (*user_v1.RegisterResponse, error) {
	userID, err := h.svc.Register(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &user_v1.RegisterResponse{UserId: userID}, nil
}

func (h *UserHandler) Login(ctx context.Context, req *user_v1.LoginRequest) (*user_v1.LoginResponse, error) {
	access, refresh, err := h.svc.Login(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &user_v1.LoginResponse{AccessToken: access, RefreshToken: refresh}, nil
}

func (h *UserHandler) Logout(ctx context.Context, req *user_v1.LogoutRequest) (*user_v1.LogoutResponse, error) {
	if err := h.svc.Logout(ctx, req.GetSessionId()); err != nil {
		return nil, toGRPCError(err)
	}
	return &user_v1.LogoutResponse{}, nil
}

func (h *UserHandler) ValidateSession(ctx context.Context, req *user_v1.ValidateSessionRequest) (*user_v1.ValidateSessionResponse, error) {
	userID, sid, err := h.svc.ValidateSession(ctx, req.GetAccessToken())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &user_v1.ValidateSessionResponse{UserId: userID, SessionId: sid}, nil
}

func (h *UserHandler) RefreshToken(ctx context.Context, req *user_v1.RefreshTokenRequest) (*user_v1.RefreshTokenResponse, error) {
	access, err := h.svc.RefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &user_v1.RefreshTokenResponse{AccessToken: access}, nil
}

func (h *UserHandler) GetLimit(ctx context.Context, req *user_v1.GetLimitRequest) (*user_v1.GetLimitResponse, error) {
	limit, err := h.svc.GetLimit(ctx, req.GetUserId())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &user_v1.GetLimitResponse{LinksLimit: limit}, nil
}

func toGRPCError(err error) error {
	switch {
	case errors.Is(err, service.ErrEmailExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, service.ErrInvalidEmail), errors.Is(err, service.ErrPasswordTooShort):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrUserNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, service.ErrInvalidCredentials), errors.Is(err, service.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
