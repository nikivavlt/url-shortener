package client

import (
	"context"
	"time"

	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type URLClient struct {
	api     urlv1.URLServiceClient
	conn    *grpc.ClientConn
	timeout time.Duration
}

func DialURL(ctx context.Context, address string) (*URLClient, error) {
	conn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &URLClient{api: urlv1.NewURLServiceClient(conn), conn: conn, timeout: defaultTimeout}, nil
}

func NewURL(api urlv1.URLServiceClient) *URLClient {
	return &URLClient{api: api, timeout: defaultTimeout}
}

func (c *URLClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *URLClient) CreateShortURL(ctx context.Context, request *urlv1.CreateShortURLRequest) (*urlv1.CreateShortURLResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.CreateShortURL(ctx, request)
}

func (c *URLClient) ListUserURLs(ctx context.Context, request *urlv1.ListUserURLsRequest) (*urlv1.ListUserURLsResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.ListUserURLs(ctx, request)
}

func (c *URLClient) DeleteURL(ctx context.Context, request *urlv1.DeleteURLRequest) (*urlv1.DeleteURLResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.DeleteURL(ctx, request)
}

func (c *URLClient) Redirect(ctx context.Context, request *urlv1.RedirectRequest) (*urlv1.RedirectResponse, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.api.Redirect(ctx, request)
}

func (c *URLClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}
