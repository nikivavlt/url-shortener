package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
	"github.com/nikivavlt/url-shortener/url/internal/domain"
)

const defaultTimeout = 2 * time.Second

type UserClient struct {
	api     userv1.UserServiceClient
	timeout time.Duration
}

func New(api userv1.UserServiceClient) *UserClient {
	return &UserClient{api: api, timeout: defaultTimeout}
}

func (c *UserClient) GetLimit(ctx context.Context, userID string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	response, err := c.api.GetLimit(ctx, &userv1.GetLimitRequest{UserId: userID})
	if err != nil {
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded:
			return 0, fmt.Errorf("%w: %v", domain.ErrUserServiceUnavailable, err)
		case codes.NotFound:
			return 0, domain.ErrUserNotFound
		default:
			return 0, fmt.Errorf("user.GetLimit: %w", err)
		}
	}
	return int64(response.GetLinksLimit()), nil
}
