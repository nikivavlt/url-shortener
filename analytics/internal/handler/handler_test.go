package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
	"github.com/nikivavlt/url-shortener/analytics/internal/service"
	analytics_v1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/analytics/v1"
	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
)

type fakeRepo struct {
	result *domain.Analytics
	err    error
}

func (r *fakeRepo) GetAnalytics(context.Context, string, int) (*domain.Analytics, error) {
	return r.result, r.err
}

type fakeURLs struct {
	resp *urlv1.GetURLResponse
	err  error
}

func (u *fakeURLs) GetURL(context.Context, *urlv1.GetURLRequest) (*urlv1.GetURLResponse, error) {
	return u.resp, u.err
}

func newHandler(repo *fakeRepo, urls *fakeURLs) *AnalyticsHandler {
	return New(service.New(repo, urls))
}

func TestGetAnalytics_MapsResponse(t *testing.T) {
	repo := &fakeRepo{result: &domain.Analytics{
		TotalClicks: 10,
		ByDay: []domain.DayCount{
			{Date: time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), Count: 4},
			{Date: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC), Count: 6},
		},
		TopReferers: []domain.RefererCount{{Referer: "https://a.example", Count: 7}, {Referer: "", Count: 3}},
	}}
	h := newHandler(repo, &fakeURLs{resp: &urlv1.GetURLResponse{UserId: "u1"}})

	resp, err := h.GetAnalytics(context.Background(), &analytics_v1.GetAnalyticsRequest{ShortCode: "abc", UserId: "u1", Days: 7})
	if err != nil {
		t.Fatal(err)
	}

	if resp.GetTotalClicks() != 10 {
		t.Fatalf("total = %d, want 10", resp.GetTotalClicks())
	}
	days := resp.GetClicksByDay()
	if len(days) != 2 || days[0].GetDate() != "2026-11-26" || days[0].GetClicks() != 4 || days[1].GetDate() != "2026-11-25" {
		t.Fatalf("clicks by day = %+v", days)
	}
	refs := resp.GetTopReferers()
	if len(refs) != 2 || refs[0].GetReferer() != "https://a.example" || refs[0].GetClicks() != 7 || refs[1].GetClicks() != 3 {
		t.Fatalf("top referers = %+v", refs)
	}
}

func TestGetAnalytics_Errors(t *testing.T) {
	tests := []struct {
		name string
		req  *analytics_v1.GetAnalyticsRequest
		repo *fakeRepo
		urls *fakeURLs
		want codes.Code
	}{
		{
			name: "missing short code",
			req:  &analytics_v1.GetAnalyticsRequest{UserId: "u1"},
			repo: &fakeRepo{}, urls: &fakeURLs{},
			want: codes.InvalidArgument,
		},
		{
			name: "missing user id",
			req:  &analytics_v1.GetAnalyticsRequest{ShortCode: "abc"},
			repo: &fakeRepo{}, urls: &fakeURLs{},
			want: codes.InvalidArgument,
		},
		{
			name: "url not found",
			req:  &analytics_v1.GetAnalyticsRequest{ShortCode: "abc", UserId: "u1"},
			repo: &fakeRepo{}, urls: &fakeURLs{err: status.Error(codes.NotFound, "x")},
			want: codes.NotFound,
		},
		{
			name: "not the owner",
			req:  &analytics_v1.GetAnalyticsRequest{ShortCode: "abc", UserId: "u1"},
			repo: &fakeRepo{}, urls: &fakeURLs{resp: &urlv1.GetURLResponse{UserId: "other"}},
			want: codes.PermissionDenied,
		},
		{
			name: "repository failure",
			req:  &analytics_v1.GetAnalyticsRequest{ShortCode: "abc", UserId: "u1"},
			repo: &fakeRepo{err: errors.New("clickhouse down")},
			urls: &fakeURLs{resp: &urlv1.GetURLResponse{UserId: "u1"}},
			want: codes.Internal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newHandler(tt.repo, tt.urls).GetAnalytics(context.Background(), tt.req)
			if got := status.Code(err); got != tt.want {
				t.Fatalf("code = %v, want %v (err=%v)", got, tt.want, err)
			}
		})
	}
}

func TestGetAnalytics_InternalErrorDoesNotLeakDetails(t *testing.T) {
	h := newHandler(
		&fakeRepo{err: errors.New("dial tcp 10.0.0.5:9000: refused")},
		&fakeURLs{resp: &urlv1.GetURLResponse{UserId: "u1"}},
	)
	_, err := h.GetAnalytics(context.Background(), &analytics_v1.GetAnalyticsRequest{ShortCode: "abc", UserId: "u1"})
	if msg := status.Convert(err).Message(); msg != "internal error" {
		t.Fatalf("message = %q, want generic \"internal error\"", msg)
	}
}
