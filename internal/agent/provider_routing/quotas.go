package provider_routing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QuotaTracker manages per-provider usage state with deterministic minute/day reset.
type QuotaTracker struct {
	mu    sync.Mutex
	usage map[string]*ProviderUsageState
	db    *pgxpool.Pool // nil = in-memory only
}

// NewQuotaTracker creates a quota tracker. If db is non-nil, usage is persisted.
func NewQuotaTracker(db *pgxpool.Pool) *QuotaTracker {
	return &QuotaTracker{
		usage: make(map[string]*ProviderUsageState),
		db:    db,
	}
}

// GetUsage returns current usage for a provider, applying reset logic.
func (q *QuotaTracker) GetUsage(provider string) ProviderUsageState {
	q.mu.Lock()
	defer q.mu.Unlock()

	state := q.getOrCreate(provider)
	q.applyResets(state, time.Now())
	return *state
}

// GetAllUsage returns usage for all tracked providers.
func (q *QuotaTracker) GetAllUsage() []ProviderUsageState {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	result := make([]ProviderUsageState, 0, len(q.usage))
	for _, s := range q.usage {
		q.applyResets(s, now)
		result = append(result, *s)
	}
	return result
}

// RecordUsage adds a request + token usage for a provider.
func (q *QuotaTracker) RecordUsage(ctx context.Context, provider string, tokens int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	state := q.getOrCreate(provider)
	now := time.Now()
	q.applyResets(state, now)

	state.RequestsThisMinute++
	state.RequestsToday++
	if tokens > 0 {
		state.TokensThisMinute += tokens
		state.TokensToday += tokens
	}
	state.LastUpdated = now

	if q.db != nil {
		q.persistState(ctx, state)
	}
}

// CheckQuota checks whether a projected request is within limits.
// Returns ("", true) if OK, or (reason, false) if limit would be exceeded.
// Unknown limits (0) are treated conservatively: never cause rejection.
func CheckQuota(limits ProviderLimits, usage ProviderUsageState, estimatedTokens int) (string, bool) {
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}

	// RPM check
	if limits.RPM > 0 {
		projected := usage.RequestsThisMinute + 1
		if projected > limits.RPM {
			return fmt.Sprintf("RPM exceeded: projected %d > limit %d", projected, limits.RPM), false
		}
	}

	// TPM check
	if limits.TPM > 0 && estimatedTokens > 0 {
		projected := usage.TokensThisMinute + estimatedTokens
		if projected > limits.TPM {
			return fmt.Sprintf("TPM exceeded: projected %d > limit %d", projected, limits.TPM), false
		}
	}

	// RPD check
	if limits.RPD > 0 {
		projected := usage.RequestsToday + 1
		if projected > limits.RPD {
			return fmt.Sprintf("RPD exceeded: projected %d > limit %d", projected, limits.RPD), false
		}
	}

	// TPD check
	if limits.TPD > 0 && estimatedTokens > 0 {
		projected := usage.TokensToday + estimatedTokens
		if projected > limits.TPD {
			return fmt.Sprintf("TPD exceeded: projected %d > limit %d", projected, limits.TPD), false
		}
	}

	return "", true
}

// ComputeHeadroom returns a [0,1] score representing remaining capacity.
// Higher = more headroom. Unknown limits (0) contribute 1.0 (neutral).
func ComputeHeadroom(limits ProviderLimits, usage ProviderUsageState, estimatedTokens int) float64 {
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}

	scores := make([]float64, 0, 4)

	if limits.RPM > 0 {
		projected := float64(usage.RequestsThisMinute+1) / float64(limits.RPM)
		scores = append(scores, clamp01(1.0-projected))
	}
	if limits.TPM > 0 && estimatedTokens > 0 {
		projected := float64(usage.TokensThisMinute+estimatedTokens) / float64(limits.TPM)
		scores = append(scores, clamp01(1.0-projected))
	}
	if limits.RPD > 0 {
		projected := float64(usage.RequestsToday+1) / float64(limits.RPD)
		scores = append(scores, clamp01(1.0-projected))
	}
	if limits.TPD > 0 && estimatedTokens > 0 {
		projected := float64(usage.TokensToday+estimatedTokens) / float64(limits.TPD)
		scores = append(scores, clamp01(1.0-projected))
	}

	if len(scores) == 0 {
		return 1.0 // unknown limits → neutral headroom
	}

	// Use minimum headroom (most constrained resource)
	min := scores[0]
	for _, s := range scores[1:] {
		if s < min {
			min = s
		}
	}
	return min
}

// LoadFromDB loads all persisted usage states. Called on startup.
func (q *QuotaTracker) LoadFromDB(ctx context.Context) error {
	if q.db == nil {
		return nil
	}

	rows, err := q.db.Query(ctx, `
		SELECT provider_name, requests_this_minute, tokens_this_minute,
		       requests_today, tokens_today, last_updated
		FROM agent_provider_usage
	`)
	if err != nil {
		return fmt.Errorf("load provider usage: %w", err)
	}
	defer rows.Close()

	q.mu.Lock()
	defer q.mu.Unlock()

	for rows.Next() {
		var s ProviderUsageState
		if err := rows.Scan(
			&s.ProviderName,
			&s.RequestsThisMinute, &s.TokensThisMinute,
			&s.RequestsToday, &s.TokensToday,
			&s.LastUpdated,
		); err != nil {
			return fmt.Errorf("scan provider usage: %w", err)
		}
		q.usage[s.ProviderName] = &s
	}
	return rows.Err()
}

func (q *QuotaTracker) getOrCreate(provider string) *ProviderUsageState {
	s, ok := q.usage[provider]
	if !ok {
		s = &ProviderUsageState{
			ProviderName: provider,
			LastUpdated:  time.Now(),
		}
		q.usage[provider] = s
	}
	return s
}

// applyResets zeroes minute/day counters when time boundaries cross.
func (q *QuotaTracker) applyResets(s *ProviderUsageState, now time.Time) {
	if s.LastUpdated.IsZero() {
		return
	}

	// Reset minute counters when the minute boundary changes
	if now.Truncate(time.Minute) != s.LastUpdated.Truncate(time.Minute) {
		s.RequestsThisMinute = 0
		s.TokensThisMinute = 0
	}

	// Reset daily counters when the day boundary changes (UTC)
	lastDay := s.LastUpdated.UTC().Truncate(24 * time.Hour)
	nowDay := now.UTC().Truncate(24 * time.Hour)
	if nowDay != lastDay {
		s.RequestsToday = 0
		s.TokensToday = 0
	}
}

func (q *QuotaTracker) persistState(ctx context.Context, s *ProviderUsageState) {
	if q.db == nil {
		return
	}

	_, _ = q.db.Exec(ctx, `
		INSERT INTO agent_provider_usage (
			provider_name, requests_this_minute, tokens_this_minute,
			requests_today, tokens_today, last_updated
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (provider_name) DO UPDATE SET
			requests_this_minute = $2,
			tokens_this_minute = $3,
			requests_today = $4,
			tokens_today = $5,
			last_updated = $6
	`, s.ProviderName, s.RequestsThisMinute, s.TokensThisMinute,
		s.RequestsToday, s.TokensToday, s.LastUpdated)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
