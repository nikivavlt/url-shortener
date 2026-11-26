package service

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
	urlv1 "github.com/nikivavlt/url-shortener/shared/pkg/proto/url/v1"
)

type fakeRepo struct {
	calls   int
	gotCode string
	gotDays int
	result  *domain.Analytics
	err     error
}

func (r *fakeRepo) GetAnalytics(_ context.Context, shortCode string, days int) (*domain.Analytics, error) {
	r.calls++
	r.gotCode, r.gotDays = shortCode, days
	return r.result, r.err
}

type fakeURLs struct {
	gotCode string
	resp    *urlv1.GetURLResponse
	err     error
}

func (u *fakeURLs) GetURL(_ context.Context, req *urlv1.GetURLRequest) (*urlv1.GetURLResponse, error) {
	u.gotCode = req.GetShortCode()
	return u.resp, u.err
}

func TestGetAnalytics_Success(t *testing.T) {
	want := &domain.Analytics{TotalClicks: 5}
	repo := &fakeRepo{result: want}
	urls := &fakeURLs{resp: &urlv1.GetURLResponse{UserId: "u1"}}

	got, err := New(repo, urls).GetAnalytics(context.Background(), "u1", "abc", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if urls.gotCode != "abc" || repo.gotCode != "abc" {
		t.Fatalf("short code not propagated: url=%q repo=%q", urls.gotCode, repo.gotCode)
	}
}

func TestGetAnalytics_DaysNormalization(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero uses default", 0, defaultDays},
		{"negative uses default", -5, defaultDays},
		{"in range kept", 7, 7},
		{"max kept", maxDays, maxDays},
		{"above max clamped", 1000, maxDays},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{result: &domain.Analytics{}}
			urls := &fakeURLs{resp: &urlv1.GetURLResponse{UserId: "u1"}}

			if _, err := New(repo, urls).GetAnalytics(context.Background(), "u1", "abc", tt.in); err != nil {
				t.Fatal(err)
			}
			if repo.gotDays != tt.want {
				t.Fatalf("repo got days=%d, want %d", repo.gotDays, tt.want)
			}
		})
	}
}

func TestGetAnalytics_URLNotFound(t *testing.T) {
	repo := &fakeRepo{}
	urls := &fakeURLs{err: status.Error(codes.NotFound, "no such url")}

	_, err := New(repo, urls).GetAnalytics(context.Background(), "u1", "abc", 7)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if repo.calls != 0 {
		t.Fatal("repo must not be queried when the URL does not exist")
	}
}

func TestGetAnalytics_NotOwner(t *testing.T) {
	repo := &fakeRepo{}
	urls := &fakeURLs{resp: &urlv1.GetURLResponse{UserId: "someone-else"}}

	_, err := New(repo, urls).GetAnalytics(context.Background(), "u1", "abc", 7)
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
	if repo.calls != 0 {
		t.Fatal("repo must not be queried for a non-owner")
	}
}

func TestGetAnalytics_URLServiceUnavailable(t *testing.T) {
	repo := &fakeRepo{}
	urls := &fakeURLs{err: status.Error(codes.Unavailable, "down")}

	_, err := New(repo, urls).GetAnalytics(context.Background(), "u1", "abc", 7)
	if err == nil || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("err = %v, want a generic wrapped error", err)
	}
	if repo.calls != 0 {
		t.Fatal("repo must not be queried when the URL lookup failed")
	}
}

func TestGetAnalytics_RepoErrorPropagates(t *testing.T) {
	boom := errors.New("clickhouse down")
	repo := &fakeRepo{err: boom}
	urls := &fakeURLs{resp: &urlv1.GetURLResponse{UserId: "u1"}}

	_, err := New(repo, urls).GetAnalytics(context.Background(), "u1", "abc", 7)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want repo error", err)
	}
}
