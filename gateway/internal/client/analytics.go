package client

import (
	"context"
	"time"

	analyticsv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/analytics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type AnalyticsClient struct {
	api     analyticsv1.AnalyticsServiceClient
	conn    *grpc.ClientConn
	timeout time.Duration
}

func DialAnalytics(address string) (*AnalyticsClient, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return &AnalyticsClient{api: analyticsv1.NewAnalyticsServiceClient(conn), conn: conn, timeout: defaultTimeout}, nil
}

func NewAnalytics(api analyticsv1.AnalyticsServiceClient) *AnalyticsClient {
	return &AnalyticsClient{api: api, timeout: defaultTimeout}
}

func (c *AnalyticsClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *AnalyticsClient) GetAnalytics(ctx context.Context, request *analyticsv1.GetAnalyticsRequest) (*analyticsv1.GetAnalyticsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.GetAnalytics(ctx, request)
}
