package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nikivavlt/url-shortener/analytics/internal/client"
	"github.com/nikivavlt/url-shortener/analytics/internal/config"
	"github.com/segmentio/kafka-go"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	fmt.Println(cfg)

	ctx := context.Background() // change it?

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

	db := clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{cfg.ClickHouseAddr},
		Auth: clickhouse.Auth{
			Database: cfg.ClickHouseDB,
			Username: cfg.ClickHouseUsername,
			Password: cfg.ClickHousePassword,
		},
		DialTimeout: 5 * time.Second, // change it?
	})

	// Check connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		log.Fatalf("clickhouse connection failed: %w", err)
	}

	if err := initDB(ctx, db, "click_events"); err != nil {
		// .....
	}
}
