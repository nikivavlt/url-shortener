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

const defaultTimeout = 5 * time.Second // change it?

func DialURL(address string) (*URLClient, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
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

func (c *URLClient) GetURL(ctx context.Context, request *urlv1.GetURLRequest) (*urlv1.GetURLResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.GetURL(ctx, request)
}
