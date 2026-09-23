package handler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nikivavlt/url-shortener/user/internal/service"
)

func TestToGRPCError(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want codes.Code
	}{
		{"duplicate email", service.ErrEmailExists, codes.AlreadyExists},
		{"invalid email", service.ErrInvalidEmail, codes.InvalidArgument},
		{"password too short", service.ErrPasswordTooShort, codes.InvalidArgument},
		{"user not found", service.ErrUserNotFound, codes.NotFound},
		{"invalid credentials", service.ErrInvalidCredentials, codes.Unauthenticated},
		{"revoked/expired session", service.ErrUnauthenticated, codes.Unauthenticated},
		{"unknown -> internal", errors.New("boom"), codes.Internal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, status.Code(toGRPCError(tc.in)))
		})
	}
}
