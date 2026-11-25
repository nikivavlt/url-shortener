package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nikivavlt/url-shortener/analytics/internal/client"
	"github.com/nikivavlt/url-shortener/analytics/internal/config"
	"github.com/nikivavlt/url-shortener/analytics/internal/consumer"
	"github.com/nikivavlt/url-shortener/analytics/internal/handler"
	"github.com/nikivavlt/url-shortener/analytics/internal/repository"
	"github.com/nikivavlt/url-shortener/analytics/internal/service"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	analytics_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/analytics/v1"
)

const (
	shutdownTimeout = 10 * time.Second
	batchSize       = 100
	flushInterval   = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// gRPC client
	URLClient, err := client.DialURL(cfg.URLServiceAddr)
	if err != nil {
		return fmt.Errorf("url service client: %w", err)
	}
	defer URLClient.Close()

	// ClickHouse
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.ClickHouseAddr},
		Auth: clickhouse.Auth{
			Database: cfg.ClickHouseDB,
			Username: cfg.ClickHouseUsername,
			Password: cfg.ClickHousePassword,
		},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("clickhouse open: %w", err)
	}
	defer conn.Close()

	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := conn.Ping(initCtx); err != nil {
		return fmt.Errorf("clickhouse ping: %w", err)
	}

	repo := repository.New(conn)
	if err := repo.Init(initCtx); err != nil {
		return err
	}

	// Kafka
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  cfg.KafkaBrokers,
		Topic:    "click_events",
		GroupID:  "analytics-service",
		MinBytes: 1,
		MaxBytes: 10 << 20,
		MaxWait:  time.Second,
	})
	defer reader.Close()

	// Dependencies
	cons := consumer.New(reader, repo, batchSize, flushInterval)
	svc := service.New(repo, URLClient)
	h := handler.New(svc)

	// gRPC server
	grpcServer := grpc.NewServer()
	analytics_v1.RegisterAnalyticsServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// think about this flow

	// Run consumer
	consumerDone := make(chan struct{})
	var consumerErr error
	go func() {
		defer close(consumerDone)
		consumerErr = cons.Run(ctx)
	}()

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("analytics service listening on :%s", cfg.GRPCPort)
		serverErr <- grpcServer.Serve(lis)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		log.Println("shutting down...")
	case err := <-serverErr:
		runErr = fmt.Errorf("grpc serve: %w", err)
	case <-consumerDone:
		if consumerErr != nil {
			runErr = fmt.Errorf("consumer: %w", consumerErr)
		}
	}

	stop()

	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()

	select {
	case <-grpcDone:
	case <-time.After(shutdownTimeout):
		log.Println("graceful shutdown timed out")
		grpcServer.Stop()
	}

	select {
	case <-consumerDone:
	case <-time.After(shutdownTimeout):
		log.Println("consumer did not stop in time")
	}

	return runErr
}
