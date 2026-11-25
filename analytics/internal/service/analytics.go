package service

import (
	"context"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
)

type AnalyticsService struct {
}

type Repository interface {
	InsertBatch(ctx context.Context, events []domain.ClickEvent) error
	GetAnalytics(ctx context.Context)
}

type UrlClient interface {
	GetURL(ctx context.Context, request *urlv1.GetURLRequest) (*urlv1.GetURLResponse, error)
}

func (s *AnalyticsService) New(repo Repository, urlClient UrlClient) {

}

func (s *AnalyticsService) GetAnalytics(ctx context.Context, userId, shortCode string, days int) {

}

// func do() {
// for {
//     batch, err := broker.ReadMessages(ctx, 100, 5*time.Second)
//     if err != nil {
//         return err
//     }

//     if len(batch.Events) == 0 {
//         continue
//     }

//     if err := clickhouseRepo.InsertBatch(ctx, batch.Events); err != nil {
//         return err
//     }

//     if err := broker.CommitMessages(ctx, batch.Messages); err != nil {
//         return err
//     }
// }
// }
