package main

import (
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	user_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
	"github.com/nikivavlt/url-shortener/user/internal/auth"
	"github.com/nikivavlt/url-shortener/user/internal/cache"
	"github.com/nikivavlt/url-shortener/user/internal/config"
	"github.com/nikivavlt/url-shortener/user/internal/handler"
	"github.com/nikivavlt/url-shortener/user/internal/postgre"
	"github.com/nikivavlt/url-shortener/user/internal/redis"
	"github.com/nikivavlt/url-shortener/user/internal/repository"
	"github.com/nikivavlt/url-shortener/user/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := runMigrations(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	pool, err := postgre.NewPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	rdb, err := redis.NewClient(cfg.RedisHost, cfg.RedisPort)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer func() {
		if cerr := rdb.Close(); cerr != nil {
			log.Printf("redis close: %v", cerr)
		}
	}()

	cache := cache.New(rdb)
	repo := repository.New(pool)
	authn := auth.New(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	svc := service.New(repo, cache, authn, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	h := handler.New(svc)

	grpcServer := grpc.NewServer()
	user_v1.RegisterUserServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":"+cfg.UserServicePort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	go func() {
		log.Printf("user service listening on :%s", cfg.UserServicePort)
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	grpcServer.GracefulStop()
}
