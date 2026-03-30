package messaging

import (
	"fmt"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
)

// SetupStreams creates the required JetStream streams for the Runeforge platform.
func SetupStreams(js nats.JetStreamContext) error {
	cfg := &nats.StreamConfig{
		Name:      "RUNEFORGE",
		Subjects:  []string{subjects.SubjectWildcard},
		MaxAge:    7 * 24 * time.Hour,
		Storage:   nats.FileStorage,
		Replicas:  1,
		Retention: nats.LimitsPolicy,
	}

	_, err := js.StreamInfo("RUNEFORGE")
	if err == nats.ErrStreamNotFound {
		if _, err := js.AddStream(cfg); err != nil {
			return fmt.Errorf("create RUNEFORGE stream: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("check RUNEFORGE stream: %w", err)
	}

	// Stream exists; update config to ensure it matches.
	if _, err := js.UpdateStream(cfg); err != nil {
		return fmt.Errorf("update RUNEFORGE stream: %w", err)
	}
	return nil
}
