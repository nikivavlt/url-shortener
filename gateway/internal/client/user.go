package client

import (
	"context"
	"time"

	userv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultTimeout = 5 * time.Second

type UserClient struct {
	api     userv1.UserServiceClient
	conn    *grpc.ClientConn
	timeout time.Duration
}

func DialUser(address string) (*UserClient, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return &UserClient{api: userv1.NewUserServiceClient(conn), conn: conn, timeout: defaultTimeout}, nil
}

func NewUser(api userv1.UserServiceClient) *UserClient {
	return &UserClient{api: api, timeout: defaultTimeout}
}

func (c *UserClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *UserClient) Register(ctx context.Context, request *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.Register(ctx, request)
}

func (c *UserClient) Login(ctx context.Context, request *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.Login(ctx, request)
}

func (c *UserClient) Logout(ctx context.Context, request *userv1.LogoutRequest) (*userv1.LogoutResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.Logout(ctx, request)
}

func (c *UserClient) ValidateSession(ctx context.Context, request *userv1.ValidateSessionRequest) (*userv1.ValidateSessionResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.ValidateSession(ctx, request)
}

func (c *UserClient) RefreshToken(ctx context.Context, request *userv1.RefreshTokenRequest) (*userv1.RefreshTokenResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.RefreshToken(ctx, request)
}

func (c *UserClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}
