package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
	userv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"

	"github.com/nikivavlt/url-shortener/url/internal/cache"
	"github.com/nikivavlt/url-shortener/url/internal/client"
	"github.com/nikivavlt/url-shortener/url/internal/config"
	"github.com/nikivavlt/url-shortener/url/internal/handler"
	"github.com/nikivavlt/url-shortener/url/internal/repository"
	"github.com/nikivavlt/url-shortener/url/internal/service"
)

const shutdownTimeout = 10 * time.Second

const collectionName = "links"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// MongoDB
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = mongoClient.Disconnect(ctx)
	}()

	if err := mongoClient.Ping(ctx, nil); err != nil {
		log.Fatalf("mongo ping: %v", err)
	}

	db := mongoClient.Database(cfg.MongoDB)
	if err := initDB(ctx, db, collectionName); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	// Redis
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis ping: %v", err)
	}

	// User service
	userConn, err := grpc.NewClient(
		cfg.UserServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("user service client: %v", err)
	}
	defer userConn.Close()

	// Dependencies
	repo := repository.New(db.Collection(collectionName))
	cache := cache.New(rdb)
	users := client.New(userv1.NewUserServiceClient(userConn))
	svc := service.New(repo, cache, users, cfg.ShortURLBase)
	h := handler.New(svc)

	// gRPC
	grpcServer := grpc.NewServer()
	urlv1.RegisterURLServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go func() {
		log.Printf("url service listening on :%s", cfg.GRPCPort)

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
