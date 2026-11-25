package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nikivavlt/url-shortener/analytics/internal/broker"
	"github.com/nikivavlt/url-shortener/analytics/internal/client"
	"github.com/nikivavlt/url-shortener/analytics/internal/config"
	"github.com/nikivavlt/url-shortener/analytics/internal/handler"
	"github.com/nikivavlt/url-shortener/analytics/internal/repository"
	"github.com/nikivavlt/url-shortener/analytics/internal/service"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	analytics_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/analytics/v1"
	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
)

const shutdownTimeout = 10 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background() // change it?

	// gRPC client
	urlClient, err := client.DialURL(cfg.UrlServiceAddr)
	if err != nil {
		log.Fatalf("url service client: %v", err)
	}
	defer urlClient.Close()

	// Kafka
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.KafkaBrokers, Topic: "click_events", GroupID: "analytics-service",
	})
	defer reader.Close()

	// ClickHouse
	db := clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{cfg.ClickHouseAddr},
		Auth: clickhouse.Auth{
			Database: cfg.ClickHouseDB,
			Username: cfg.ClickHouseUsername,
			Password: cfg.ClickHousePassword,
		},
		DialTimeout: 5 * time.Second, // change it?
	})

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		log.Fatalf("clickhouse connection failed: %w", err)
	}

	if err := initDB(ctx, db, "click_events"); err != nil {
		// .....
	}

	// Dependencies
	repo := repository.New(db)
	consumer := broker.New(reader)
	svc := service.New(repo, urlClient)
	h := handler.New(svc)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop() // move it?

	// gRPC server
	grpcServer := grpc.NewServer()
	analytics_v1.RegisterAnalyticsServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":"+cfg.AnalyticsServicerPort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go func() {
		log.Printf("analytics service listening on :%s", cfg.AnalyticsServicerPort)

		if err := grpcServer.Serve(lis); err != nil &&
			!errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()

	log.Println("shutting down...")

	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(shutdownTimeout):
		log.Println("graceful shutdown timed out")
		grpcServer.Stop()
	}
}
