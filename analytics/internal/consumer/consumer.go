package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
	"github.com/segmentio/kafka-go"
)

type Repository interface {
	InsertBatch(ctx context.Context, events []domain.ClickEvent) error
}

type Consumer struct {
	reader        *kafka.Reader
	repo          Repository
	batchSize     int
	flushInterval time.Duration
}

func New(reader *kafka.Reader, repo Repository, batchSize int, flushInterval time.Duration) *Consumer {
	return &Consumer{reader: reader, repo: repo, batchSize: batchSize, flushInterval: flushInterval}
}

func (c *Consumer) Run(ctx context.Context) error {
	for {
		events, messages, err := c.readBatch(ctx)
		if err == nil && len(messages) > 0 {
			if err = c.repo.InsertBatch(ctx, events); err == nil {
				err = c.reader.CommitMessages(ctx, messages...)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func (c *Consumer) readBatch(ctx context.Context) ([]domain.ClickEvent, []kafka.Message, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, c.flushInterval)
	defer cancel()

	var (
		events   []domain.ClickEvent
		messages []kafka.Message
	)

	for len(events) < c.batchSize {
		msg, err := c.reader.FetchMessage(fetchCtx)
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("fetch kafka message: %w", err)
		}
		messages = append(messages, msg)

		clickEvent := domain.ClickEvent{}

		if err := json.Unmarshal(msg.Value, &clickEvent); err != nil {
			log.Printf("skip undecodable message partition=%d offset=%d: %v", msg.Partition, msg.Offset, err)
			continue
		}

		// log.Printf("Received click event: %+v", event)
		events = append(events, clickEvent)
	}

	return events, messages, nil
}
