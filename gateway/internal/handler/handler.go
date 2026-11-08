package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nikivavlt/url-shortener/gateway/internal/middleware"
	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
	userv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/user/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxBodyBytes    = 1 << 20
)

type userClient interface {
	Register(context.Context, *userv1.RegisterRequest) (*userv1.RegisterResponse, error)
	Login(context.Context, *userv1.LoginRequest) (*userv1.LoginResponse, error)
	Logout(context.Context, *userv1.LogoutRequest) (*userv1.LogoutResponse, error)
	RefreshToken(context.Context, *userv1.RefreshTokenRequest) (*userv1.RefreshTokenResponse, error)
}

type urlClient interface {
	CreateShortURL(context.Context, *urlv1.CreateShortURLRequest) (*urlv1.CreateShortURLResponse, error)
	ListUserURLs(context.Context, *urlv1.ListUserURLsRequest) (*urlv1.ListUserURLsResponse, error)
	DeleteURL(context.Context, *urlv1.DeleteURLRequest) (*urlv1.DeleteURLResponse, error)
	Redirect(context.Context, *urlv1.RedirectRequest) (*urlv1.RedirectResponse, error)
}

type Handler struct {
	users userClient
	urls  urlClient
}

func New(users userClient, urls urlClient) *Handler { return &Handler{users: users, urls: urls} }

func (h *Handler) Router(auth func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/auth/register", h.register)
	r.Post("/api/v1/auth/login", h.login)
	r.Post("/api/v1/auth/refresh", h.refresh)
	r.Get("/{code}", h.redirect)
	r.Group(func(r chi.Router) { // group?
		r.Use(auth)
		r.Post("/api/v1/auth/logout", h.logout)
		r.Post("/api/v1/urls", h.createURL)
		r.Get("/api/v1/urls", h.listURLs)
		r.Delete("/api/v1/urls/{code}", h.deleteURL)
	})
	return r
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var body credentialsRequest
	if decodeJSON(w, r, &body) != nil {
		return
	}
	response, err := h.users.Register(r.Context(), &userv1.RegisterRequest{Email: body.Email, Password: body.Password})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, registerResponse{UserID: response.GetUserId()})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body credentialsRequest
	if decodeJSON(w, r, &body) != nil {
		return
	}
	response, err := h.users.Login(r.Context(), &userv1.LoginRequest{Email: body.Email, Password: body.Password})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: response.GetAccessToken(), RefreshToken: response.GetRefreshToken()})
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if decodeJSON(w, r, &body) != nil {
		return
	}
	response, err := h.users.RefreshToken(r.Context(), &userv1.RefreshTokenRequest{RefreshToken: body.RefreshToken})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: response.GetAccessToken()})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	_, err := h.users.Logout(r.Context(), &userv1.LogoutRequest{SessionId: middleware.SessionID(r.Context())})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (h *Handler) createURL(w http.ResponseWriter, r *http.Request) {
	var body createURLRequest
	if decodeJSON(w, r, &body) != nil {
		return
	}
	response, err := h.urls.CreateShortURL(r.Context(), &urlv1.CreateShortURLRequest{
		OriginalUrl: body.OriginalURL, UserId: middleware.UserID(r.Context()), ExpiresIn: body.ExpiresIn,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createURLResponse{ShortCode: response.GetShortCode(), ShortURL: response.GetShortUrl()})
}

func (h *Handler) listURLs(w http.ResponseWriter, r *http.Request) {
	limit, err := pageLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	response, err := h.urls.ListUserURLs(r.Context(), &urlv1.ListUserURLsRequest{
		UserId: middleware.UserID(r.Context()), Limit: limit, Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	urls := make([]urlItemResponse, 0, len(response.GetUrls()))
	for _, item := range response.GetUrls() {
		urls = append(urls, urlItemResponse{ShortCode: item.GetShortCode(), OriginalURL: item.GetOriginalUrl(), CreatedAt: unixTime(item.GetCreatedAt()), ExpiresAt: unixTime(item.GetExpiresAt()), IsActive: item.GetIsActive()})
	}
	writeJSON(w, http.StatusOK, listURLsResponse{URLs: urls, NextCursor: response.GetNextCursor()})
}

func (h *Handler) deleteURL(w http.ResponseWriter, r *http.Request) {
	_, err := h.urls.DeleteURL(r.Context(), &urlv1.DeleteURLRequest{ShortCode: chi.URLParam(r, "code"), UserId: middleware.UserID(r.Context())})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request) {
	response, err := h.urls.Redirect(r.Context(), &urlv1.RedirectRequest{ShortCode: chi.URLParam(r, "code"), Ip: requestIP(r), UserAgent: r.UserAgent(), Referer: r.Referer()})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	w.Header().Set("Location", response.GetOriginalUrl())
	w.WriteHeader(http.StatusFound)
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}
type createURLRequest struct {
	OriginalURL string `json:"original_url"`
	ExpiresIn   *int64 `json:"expires_in,omitempty"`
}
type registerResponse struct {
	UserID string `json:"user_id"`
}
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
}
type createURLResponse struct {
	ShortCode string `json:"short_code"`
	ShortURL  string `json:"short_url"`
}
type listURLsResponse struct {
	URLs       []urlItemResponse `json:"urls"`
	NextCursor string            `json:"next_cursor"`
}
type urlItemResponse struct {
	ShortCode   string  `json:"short_code"`
	OriginalURL string  `json:"original_url"`
	CreatedAt   *string `json:"created_at"`
	ExpiresAt   *string `json:"expires_at"`
	IsActive    bool    `json:"is_active"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return err
	}
	return nil
}

func pageLimit(value string) (int32, error) {
	if value == "" {
		return defaultPageSize, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 {
		return 0, errors.New("limit must be a positive integer")
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	return int32(limit), nil
}

func requestIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func unixTime(value int64) *string {
	if value == 0 {
		return nil
	}
	formatted := time.Unix(value, 0).UTC().Format(time.RFC3339)
	return &formatted
}

func writeGRPCError(w http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	switch status.Code(err) {
	case codes.InvalidArgument:
		statusCode = http.StatusBadRequest
	case codes.Unauthenticated:
		statusCode = http.StatusUnauthorized
	case codes.PermissionDenied:
		statusCode = http.StatusForbidden
	case codes.NotFound:
		statusCode = http.StatusNotFound
	case codes.AlreadyExists:
		statusCode = http.StatusConflict
	case codes.ResourceExhausted:
		statusCode = http.StatusTooManyRequests
	case codes.Unavailable:
		statusCode = http.StatusServiceUnavailable
	}
	writeError(w, statusCode, status.Convert(err).Message())
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]string{"error": message})
}
func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
