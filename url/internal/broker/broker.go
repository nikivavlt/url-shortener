package broker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nikivavlt/url-shortener/url/internal/domain"
	"github.com/segmentio/kafka-go"
)

type Broker struct {
	broker *kafka.Writer
}

func New(broker *kafka.Writer) *Broker {
	return &Broker{broker: broker}
}

func (b Broker) WriteMessages(ctx context.Context, message domain.BrokerMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal click event: %w", err)
	}

	if err := b.broker.WriteMessages(ctx, kafka.Message{Value: data}); err != nil {
		return fmt.Errorf("publish click event: %w", err)
	}

	return nil
}
