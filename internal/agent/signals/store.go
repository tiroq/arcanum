package signals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RawEventStore persists raw events.
type RawEventStore struct {
	pool *pgxpool.Pool
}

// NewRawEventStore creates a RawEventStore.
func NewRawEventStore(pool *pgxpool.Pool) *RawEventStore {
	return &RawEventStore{pool: pool}
}

// Save persists a raw event.
func (s *RawEventStore) Save(ctx context.Context, e RawEvent) error {
	payloadBytes, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	const q = `
INSERT INTO agent_raw_events (id, source, event_type, payload, observed_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO NOTHING`
	_, err = s.pool.Exec(ctx, q, e.ID, e.Source, e.EventType, payloadBytes, e.ObservedAt, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("save raw event: %w", err)
	}
	return nil
}

// List returns recent raw events.
func (s *RawEventStore) List(ctx context.Context, limit, offset int) ([]RawEvent, error) {
	const q = `
SELECT id, source, event_type, payload, observed_at, created_at
FROM agent_raw_events
ORDER BY observed_at DESC
LIMIT $1 OFFSET $2`
	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list raw events: %w", err)
	}
	defer rows.Close()

	var out []RawEvent
	for rows.Next() {
		var e RawEvent
		var payloadBytes []byte
		if err := rows.Scan(&e.ID, &e.Source, &e.EventType, &payloadBytes, &e.ObservedAt, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan raw event: %w", err)
		}
		if len(payloadBytes) > 0 {
			_ = json.Unmarshal(payloadBytes, &e.Payload)
		}
		if e.Payload == nil {
			e.Payload = map[string]any{}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SignalStore persists normalised signals.
type SignalStore struct {
	pool *pgxpool.Pool
}

// NewSignalStore creates a SignalStore.
func NewSignalStore(pool *pgxpool.Pool) *SignalStore {
	return &SignalStore{pool: pool}
}

// Save persists a normalised signal.
func (s *SignalStore) Save(ctx context.Context, sig Signal) error {
	const q = `
INSERT INTO agent_signals (id, signal_type, severity, confidence, value, source, context_tags, observed_at, raw_event_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO NOTHING`
	_, err := s.pool.Exec(ctx, q,
		sig.ID, sig.SignalType, sig.Severity, sig.Confidence, sig.Value,
		sig.Source, sig.ContextTags, sig.ObservedAt, sig.RawEventID, sig.CreatedAt)
	if err != nil {
		return fmt.Errorf("save signal: %w", err)
	}
	return nil
}

// ListActive returns recent signals within the given window.
func (s *SignalStore) ListActive(ctx context.Context, window time.Duration, limit int) ([]Signal, error) {
	const q = `
SELECT id, signal_type, severity, confidence, value, source, context_tags, observed_at, COALESCE(raw_event_id, ''), created_at
FROM agent_signals
WHERE observed_at >= $1
ORDER BY observed_at DESC
LIMIT $2`
	cutoff := time.Now().UTC().Add(-window)
	rows, err := s.pool.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list active signals: %w", err)
	}
	defer rows.Close()

	var out []Signal
	for rows.Next() {
		var sig Signal
		if err := rows.Scan(&sig.ID, &sig.SignalType, &sig.Severity, &sig.Confidence,
			&sig.Value, &sig.Source, &sig.ContextTags, &sig.ObservedAt, &sig.RawEventID, &sig.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan signal: %w", err)
		}
		if sig.ContextTags == nil {
			sig.ContextTags = []string{}
		}
		out = append(out, sig)
	}
	return out, rows.Err()
}

// ListAll returns recent signals with pagination.
func (s *SignalStore) ListAll(ctx context.Context, limit, offset int) ([]Signal, error) {
	const q = `
SELECT id, signal_type, severity, confidence, value, source, context_tags, observed_at, COALESCE(raw_event_id, ''), created_at
FROM agent_signals
ORDER BY observed_at DESC
LIMIT $1 OFFSET $2`
	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list signals: %w", err)
	}
	defer rows.Close()

	var out []Signal
	for rows.Next() {
		var sig Signal
		if err := rows.Scan(&sig.ID, &sig.SignalType, &sig.Severity, &sig.Confidence,
			&sig.Value, &sig.Source, &sig.ContextTags, &sig.ObservedAt, &sig.RawEventID, &sig.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan signal: %w", err)
		}
		if sig.ContextTags == nil {
			sig.ContextTags = []string{}
		}
		out = append(out, sig)
	}
	return out, rows.Err()
}

// DerivedStateStore persists derived state metrics.
type DerivedStateStore struct {
	pool *pgxpool.Pool
}

// NewDerivedStateStore creates a DerivedStateStore.
func NewDerivedStateStore(pool *pgxpool.Pool) *DerivedStateStore {
	return &DerivedStateStore{pool: pool}
}

// Upsert writes a derived state key–value pair, creating or updating.
func (s *DerivedStateStore) Upsert(ctx context.Context, key string, value float64) error {
	const q = `
INSERT INTO agent_derived_state (key, value, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, q, key, value, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("upsert derived state %s: %w", key, err)
	}
	return nil
}

// GetAll returns all derived state entries as a map.
func (s *DerivedStateStore) GetAll(ctx context.Context) (map[string]float64, error) {
	const q = `SELECT key, value FROM agent_derived_state ORDER BY key`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get all derived state: %w", err)
	}
	defer rows.Close()

	out := make(map[string]float64)
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan derived state: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// GetAllEntries returns all derived state entries as a slice.
func (s *DerivedStateStore) GetAllEntries(ctx context.Context) ([]DerivedState, error) {
	const q = `SELECT key, value, updated_at FROM agent_derived_state ORDER BY key`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get all derived state entries: %w", err)
	}
	defer rows.Close()

	var out []DerivedState
	for rows.Next() {
		var ds DerivedState
		if err := rows.Scan(&ds.Key, &ds.Value, &ds.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan derived state entry: %w", err)
		}
		out = append(out, ds)
	}
	return out, rows.Err()
}
