package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nikivavlt/url-shortener/gateway/internal/middleware"
	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
	userv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
)

func TestGatewayRESTScenario(t *testing.T) {
	users := &stubUsers{}
	urls := &stubURLs{}
	router := New(users, urls).Router(middleware.NewAuth(users))

	response := serve(router, http.MethodPost, "/api/v1/auth/register", `{"email":"test@example.com","password":"password123"}`, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", response.Code, http.StatusCreated)
	}
	assertJSONField(t, response, "user_id", "user-1")

	response = serve(router, http.MethodPost, "/api/v1/auth/login", `{"email":"test@example.com","password":"password123"}`, "")
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", response.Code, http.StatusOK)
	}
	assertJSONField(t, response, "access_token", "access")

	response = serve(router, http.MethodPost, "/api/v1/urls", `{"original_url":"https://example.com","expires_in":3600}`, "Bearer access")
	if response.Code != http.StatusCreated {
		t.Fatalf("create URL status = %d, want %d", response.Code, http.StatusCreated)
	}
	if got := urls.createRequest.GetUserId(); got != "user-1" {
		t.Fatalf("CreateShortURL user_id = %q, want user-1", got)
	}
	if got := urls.createRequest.GetExpiresIn(); got != 3600 {
		t.Fatalf("CreateShortURL expires_in = %d, want 3600", got)
	}

	response = serve(router, http.MethodGet, "/api/v1/urls?limit=500", "", "Bearer access")
	if response.Code != http.StatusOK {
		t.Fatalf("list URLs status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := urls.listRequest.GetLimit(); got != maxPageSize {
		t.Fatalf("ListUserURLs limit = %d, want %d", got, maxPageSize)
	}
	if !strings.Contains(response.Body.String(), `"created_at":"2026-01-15T14:32:00Z"`) {
		t.Fatalf("created_at was not encoded as ISO-8601: %s", response.Body.String())
	}

	response = serve(router, http.MethodDelete, "/api/v1/urls/abc1234", "", "Bearer access")
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete URL status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := urls.deleteRequest.GetUserId(); got != "user-1" {
		t.Fatalf("DeleteURL user_id = %q, want user-1", got)
	}

	request := httptest.NewRequest(http.MethodGet, "/abc1234", nil)
	request.RemoteAddr = "192.0.2.1:54321"
	request.Header.Set("X-Forwarded-For", "185.86.151.42, 10.0.0.1")
	request.Header.Set("User-Agent", "gateway-test")
	request.Header.Set("Referer", "https://t.me/example")
	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, request)
	if redirect.Code != http.StatusFound || redirect.Header().Get("Location") != "https://example.com" {
		t.Fatalf("redirect = %d Location=%q", redirect.Code, redirect.Header().Get("Location"))
	}
	if got := urls.redirectRequest.GetIp(); got != "185.86.151.42" {
		t.Fatalf("Redirect ip = %q, want first X-Forwarded-For IP", got)
	}
	if urls.redirectRequest.GetUserAgent() != "gateway-test" || urls.redirectRequest.GetReferer() != "https://t.me/example" {
		t.Fatalf("redirect metadata = %#v", urls.redirectRequest)
	}
}

func TestProtectedRouteRejectsMissingBearerToken(t *testing.T) {
	users := &stubUsers{}
	urls := &stubURLs{}
	response := serve(New(users, urls).Router(middleware.NewAuth(users)), http.MethodPost, "/api/v1/urls", `{"original_url":"https://example.com"}`, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if urls.createRequest != nil {
		t.Fatal("CreateShortURL called without a validated session")
	}
}

func serve(router http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertJSONField(t *testing.T, response *httptest.ResponseRecorder, field, want string) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body[field]; got != want {
		t.Fatalf("%s = %q, want %q", field, got, want)
	}
}

type stubUsers struct{}

func (s *stubUsers) Register(context.Context, *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	return &userv1.RegisterResponse{UserId: "user-1"}, nil
}

func (s *stubUsers) Login(context.Context, *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	return &userv1.LoginResponse{AccessToken: "access", RefreshToken: "refresh"}, nil
}

func (s *stubUsers) Logout(context.Context, *userv1.LogoutRequest) (*userv1.LogoutResponse, error) {
	return &userv1.LogoutResponse{}, nil
}

func (s *stubUsers) RefreshToken(context.Context, *userv1.RefreshTokenRequest) (*userv1.RefreshTokenResponse, error) {
	return &userv1.RefreshTokenResponse{AccessToken: "access"}, nil
}

func (s *stubUsers) ValidateSession(_ context.Context, request *userv1.ValidateSessionRequest) (*userv1.ValidateSessionResponse, error) {
	if request.GetAccessToken() != "access" {
		return nil, context.Canceled
	}
	return &userv1.ValidateSessionResponse{UserId: "user-1", SessionId: "session-1"}, nil
}

type stubURLs struct {
	createRequest   *urlv1.CreateShortURLRequest
	listRequest     *urlv1.ListUserURLsRequest
	deleteRequest   *urlv1.DeleteURLRequest
	redirectRequest *urlv1.RedirectRequest
}

func (s *stubURLs) CreateShortURL(_ context.Context, request *urlv1.CreateShortURLRequest) (*urlv1.CreateShortURLResponse, error) {
	s.createRequest = request
	return &urlv1.CreateShortURLResponse{ShortCode: "abc1234", ShortUrl: "https://short.url/abc1234"}, nil
}

func (s *stubURLs) ListUserURLs(_ context.Context, request *urlv1.ListUserURLsRequest) (*urlv1.ListUserURLsResponse, error) {
	s.listRequest = request
	created := time.Date(2026, time.January, 15, 14, 32, 0, 0, time.UTC).Unix()
	return &urlv1.ListUserURLsResponse{Urls: []*urlv1.URLItem{{ShortCode: "abc1234", OriginalUrl: "https://example.com", CreatedAt: created, IsActive: true}}}, nil
}

func (s *stubURLs) DeleteURL(_ context.Context, request *urlv1.DeleteURLRequest) (*urlv1.DeleteURLResponse, error) {
	s.deleteRequest = request
	return &urlv1.DeleteURLResponse{}, nil
}

func (s *stubURLs) Redirect(_ context.Context, request *urlv1.RedirectRequest) (*urlv1.RedirectResponse, error) {
	s.redirectRequest = request
	return &urlv1.RedirectResponse{OriginalUrl: "https://example.com"}, nil
}
