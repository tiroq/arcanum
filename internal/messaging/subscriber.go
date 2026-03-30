package messaging

import (
	"fmt"

	nats "github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Subscriber subscribes to NATS JetStream subjects.
type Subscriber struct {
	js     nats.JetStreamContext
	logger *zap.Logger
}

// NewSubscriber creates a Subscriber backed by JetStream.
func NewSubscriber(nc *nats.Conn, logger *zap.Logger) (*Subscriber, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream context: %w", err)
	}
	return &Subscriber{js: js, logger: logger}, nil
}

// Subscribe creates a durable push subscription on the given subject.
func (s *Subscriber) Subscribe(subject, durable string, handler func(msg *nats.Msg)) error {
	_, err := s.js.Subscribe(subject, handler,
		nats.Durable(durable),
		nats.DeliverAll(),
		nats.AckExplicit(),
	)
	if err != nil {
		return fmt.Errorf("subscribe to %s (durable=%s): %w", subject, durable, err)
	}
	s.logger.Info("subscribed to subject",
		zap.String("subject", subject),
		zap.String("durable", durable),
	)
	return nil
}
