package consumer

import (
	"context"
	"encoding/json"
	"fmt"
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

type batch struct {
	Events   []domain.ClickEvent
	Messages []kafka.Message
}

func New(reader *kafka.Reader, repo Repository, batchSize int, flushInterval time.Duration) *Consumer {
	return &Consumer{reader: reader, repo: repo, batchSize: batchSize, flushInterval: flushInterval}
}

func (c *Consumer) Run(ctx context.Context) error {
	return nil
}

func (b *Consumer) ReadMessages(ctx context.Context, batchSize int, flushInterval time.Duration) (*batch, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("batch size must be positive")
	}

	batch := &batch{
		Events:   make([]domain.ClickEvent, 0, batchSize),
		Messages: make([]kafka.Message, 0, batchSize),
	}

	time := time.NewTimer(flushInterval)
	defer time.Stop()

	for len(batch.Events) < batchSize {
		message, err := b.reader.FetchMessage(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch kafka message: %w", err)
		}

		clickEvent := domain.ClickEvent{}

		if err := json.Unmarshal(message.Value, &clickEvent); err != nil {
			return nil, fmt.Errorf("decode kafka message: %w", err)
		}

		// log.Printf("Received click event: %+v", event)
		batch.Events = append(batch.Events, clickEvent)
		batch.Messages = append(batch.Messages, message)
	}

	return batch, nil
}

func (b *Consumer) CommitMessages(ctx context.Context, messages []kafka.Message) error {
	if len(messages) == 0 {
		return nil
	}

	if err := b.reader.CommitMessages(ctx, messages...); err != nil {
		return fmt.Errorf("commit kafka messages: %w", err)
	}

	return nil
}
