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

type Reader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

type Consumer struct {
	reader         Reader
	repo           Repository
	batchSize      int
	flushInterval  time.Duration
	processTimeout time.Duration
}

func New(
	reader Reader,
	repo Repository,
	batchSize int,
	flushInterval time.Duration,
	processTimeout time.Duration,
) *Consumer {
	return &Consumer{
		reader:         reader,
		repo:           repo,
		batchSize:      batchSize,
		flushInterval:  flushInterval,
		processTimeout: processTimeout,
	}
}

func (c *Consumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		events, messages, err := c.readBatch(ctx)
		if len(messages) > 0 {
			if processErr := c.processBatch(events, messages); processErr != nil {
				return processErr
			}
		}

		if err != nil {
			return err
		}

		if ctx.Err() != nil {
			return nil
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

	for len(messages) < c.batchSize {
		msg, err := c.reader.FetchMessage(fetchCtx)

		if err != nil {
			if ctx.Err() != nil {
				return events, messages, nil
			}

			if errors.Is(err, context.DeadlineExceeded) {
				return events, messages, nil
			}

			return events, messages, fmt.Errorf(
				"fetch kafka message: %w", err,
			)
		}

		messages = append(messages, msg)

		var event domain.ClickEvent

		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf(
				"skip undecodable message partition=%d offset=%d: %v",
				msg.Partition,
				msg.Offset,
				err,
			)
			continue
		}

		events = append(events, event)
	}

	return events, messages, nil
}

func (c *Consumer) processBatch(events []domain.ClickEvent, messages []kafka.Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.processTimeout)
	defer cancel()

	if len(events) > 0 {
		if err := c.repo.InsertBatch(ctx, events); err != nil {
			return fmt.Errorf("insert click events: %w", err)
		}
	}

	if err := c.reader.CommitMessages(ctx, messages...); err != nil {
		return fmt.Errorf("commit kafka messages: %w", err)
	}

	return nil
}
