package main

import (
	"fmt"
	"log"

	"github.com/nikivavlt/url-shortener/analytics/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	fmt.Println(cfg)
	// ctx, cancel := context.WithTimeout(
	// 	context.Background(),
	// 	10*time.Second,
	// )
	// defer cancel()

	// clickhouseDB, err := database.ConnectClickHouse(ctx)
	// if err != nil {
	// 	log.Fatalf("ClickHouse initialization failed: %v", err)
	// }
	// defer clickhouseDB.Close()

	// log.Println("ClickHouse connected and tables initialized")

	// Start your HTTP server here
}
