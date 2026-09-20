package handler

import (
	"context"

	user_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
	"github.com/nikivavlt/url-shortener/user/internal/service"
)

type UserHandler struct {
	user_v1.UnimplementedUserServiceServer
	service service.UserService
}

func NewUserHandler(service service.UserService) *UserHandler {
	return &UserHandler{service: service}
}

// type server struct {
// 	user_v1.UnimplementedUserServiceServer
// }

func (h *UserHandler) Register(ctx context.Context, req *user_v1.RegisterRequest) (*user_v1.RegisterResponse, error) {
	userID, err := h.service.Register(req.Email, req.Password)
	if err != nil {
		return nil, err
	}
	return &user_v1.RegisterResponse{UserId: userID}, nil
}

// func main() {
// 	lis, err := net.Listen("tcp", ":50051")
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	s := grpc.NewServer()
// 	user_v1.RegisterUserServiceServer(s, &server{})

// 	reflection.Register(s)

// 	fmt.Println("listening")

// 	if err := s.Serve(lis); err != nil {
// 		log.Fatal(err)
// 	}
// }
