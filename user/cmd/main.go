package main

import (
	"fmt"
	"log"
	"net"

	user_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
	"github.com/nikivavlt/url-shortener/user/internal/auth"
	"github.com/nikivavlt/url-shortener/user/internal/cache"
	"github.com/nikivavlt/url-shortener/user/internal/config"
	"github.com/nikivavlt/url-shortener/user/internal/handler"
	"github.com/nikivavlt/url-shortener/user/internal/postgre"
	"github.com/nikivavlt/url-shortener/user/internal/repository"
	"github.com/nikivavlt/url-shortener/user/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	pool, err := postgre.NewPool(cfg.DatabaseURL)
	if err != nil {

	}

	repository := repository.NewUserRepository(pool)

	auth := auth.Auth{}

	service := service.NewUserService(repository, cache.Redis{}, auth)

	handler := handler.NewUserHandler(*service) // dereference?

	lis, err := net.Listen("tcp", ":"+cfg.UserServicePort)
	if err != nil {
		log.Fatal(err)
	}

	s := grpc.NewServer()
	user_v1.RegisterUserServiceServer(s, handler)

	reflection.Register(s)

	fmt.Println("listening")

	if err := s.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
