package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/nikivavlt/url-shortener/gateway/internal/client"
	"github.com/nikivavlt/url-shortener/gateway/internal/config"
	"github.com/nikivavlt/url-shortener/gateway/internal/handler"
	"github.com/nikivavlt/url-shortener/gateway/internal/middleware"
)

const shutdownTimeout = 10 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// dial user method?
	userClient, err := client.DialUser(context.Background(), cfg.UserServiceAddr)
	if err != nil {
		log.Fatalf("user service client: %v", err)
	}
	defer userClient.Close()

	urlClient, err := client.DialURL(context.Background(), cfg.UrlServiceAddr)
	if err != nil {
		log.Fatalf("url service client: %v", err)
	}
	defer urlClient.Close()

	h := handler.New(userClient, urlClient)
	server := &http.Server{
		Addr:              ":" + cfg.GatewayPort,
		Handler:           h.Router(middleware.NewAuth(userClient)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1) // ??
	go func() {
		log.Printf("gateway listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select { // dive in this to understand
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	case <-ctx.Done():
		log.Println("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}
}
