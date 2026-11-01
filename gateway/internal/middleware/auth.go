package middleware

import (
	"context"
	"net/http"
	"strings"

	userv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
)

type validateSessionClient interface {
	ValidateSession(context.Context, *userv1.ValidateSessionRequest) (*userv1.ValidateSessionResponse, error)
}

type contextKey string

const (
	userIDKey    contextKey = "gateway.user_id"
	sessionIDKey contextKey = "gateway.session_id"
)

func NewAuth(users validateSessionClient) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				unauthorized(w)
				return
			}

			response, err := users.ValidateSession(r.Context(), &userv1.ValidateSessionRequest{AccessToken: token})
			if err != nil || response.GetUserId() == "" || response.GetSessionId() == "" {
				unauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, response.GetUserId())
			ctx = context.WithValue(ctx, sessionIDKey, response.GetSessionId())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserID(ctx context.Context) string {
	value, _ := ctx.Value(userIDKey).(string)
	return value
}

func SessionID(ctx context.Context) string {
	value, _ := ctx.Value(sessionIDKey).(string)
	return value
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
		return parts[1], true
	}
	return "", false
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}
