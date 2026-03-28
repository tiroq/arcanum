package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	nats "github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Publisher publishes events to NATS JetStream.
type Publisher struct {
	js     nats.JetStreamContext
	logger *zap.Logger
}

// NewPublisher creates a Publisher backed by JetStream.
func NewPublisher(nc *nats.Conn, logger *zap.Logger) (*Publisher, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream context: %w", err)
	}
	return &Publisher{js: js, logger: logger}, nil
}

// Publish marshals payload to JSON and publishes it on the given subject.
func (p *Publisher) Publish(ctx context.Context, subject string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload for %s: %w", subject, err)
	}

	if _, err := p.js.Publish(subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}

	p.logger.Debug("event published", zap.String("subject", subject))
	return nil
}
