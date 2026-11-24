package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
	"github.com/segmentio/kafka-go"
)

type Broker struct {
	consumer *kafka.Reader
}

type Batch struct {
	Events   []domain.ClickEvent
	Messages []kafka.Message
}

func New(reader *kafka.Reader) *Broker {
	return &Broker{consumer: reader}
}

func (b *Broker) ReadMessages(ctx context.Context, batchSize int, flushInterval time.Duration) (*Batch, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("batch size must be positive")
	}

	batch := &Batch{
		Events:   make([]domain.ClickEvent, 0, batchSize),
		Messages: make([]kafka.Message, 0, batchSize),
	}

	time := time.NewTimer(flushInterval)
	defer time.Stop()

	for len(batch.Events) < batchSize {
		message, err := b.consumer.FetchMessage(ctx)
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

func (b *Broker) CommitMessages(ctx context.Context, messages []kafka.Message) error {
	if len(messages) == 0 {
		return nil
	}

	if err := b.consumer.CommitMessages(ctx, messages...); err != nil {
		return fmt.Errorf("commit kafka messages: %w", err)
	}

	return nil
}
