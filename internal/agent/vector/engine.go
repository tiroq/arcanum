package vector

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/audit"
	"go.uber.org/zap"
)

// CacheTTL controls how long a cached vector is considered valid.
const CacheTTL = 30 * time.Second

// Engine manages the system vector lifecycle.
// It also serves as a cached VectorProvider: the vector is read from DB at
// most once per CacheTTL, and invalidated immediately on Set().
type Engine struct {
	store   StoreInterface
	auditor audit.AuditRecorder
	logger  *zap.Logger

	// Cache: RWMutex-protected, TTL-based.
	mu          sync.RWMutex
	cached      SystemVector
	cachedAt    time.Time
	cacheLoaded bool
}

// NewEngine creates a new vector engine.
func NewEngine(store StoreInterface, auditor audit.AuditRecorder, logger *zap.Logger) *Engine {
	return &Engine{store: store, auditor: auditor, logger: logger}
}

// Get returns the current system vector.
func (e *Engine) Get(ctx context.Context) (SystemVector, error) {
	return e.store.Get(ctx)
}

// Set updates the system vector, emits an audit event, and invalidates the cache.
func (e *Engine) Set(ctx context.Context, v SystemVector) error {
	v.Clamp()
	if err := e.store.Set(ctx, v); err != nil {
		return err
	}

	// Invalidate + refresh cache synchronously so concurrent readers
	// see the new vector immediately.
	e.mu.Lock()
	e.cached = v
	e.cachedAt = time.Now()
	e.cacheLoaded = true
	e.mu.Unlock()

	if e.auditor != nil {
		e.auditor.RecordEvent(ctx, "system_vector", uuid.Nil, "vector.updated", "owner", "telegram", map[string]interface{}{
			"income_priority":         v.IncomePriority,
			"family_safety_priority":  v.FamilySafetyPriority,
			"infra_priority":          v.InfraPriority,
			"automation_priority":     v.AutomationPriority,
			"exploration_level":       v.ExplorationLevel,
			"risk_tolerance":          v.RiskTolerance,
			"human_review_strictness": v.HumanReviewStrictness,
		})
	}

	e.logger.Info("system vector updated",
		zap.Float64("income_priority", v.IncomePriority),
		zap.Float64("family_safety_priority", v.FamilySafetyPriority),
		zap.Float64("risk_tolerance", v.RiskTolerance),
		zap.Float64("human_review_strictness", v.HumanReviewStrictness),
	)
	return nil
}

// GetVector implements VectorProvider. Returns a cached copy of the system
// vector, refreshed at most once per CacheTTL. Fail-open: if the store
// is unreachable, the last cached value (or defaults) are returned with a
// visible warning log — never silently.
func (e *Engine) GetVector() SystemVector {
	e.mu.RLock()
	if e.cacheLoaded && time.Since(e.cachedAt) < CacheTTL {
		v := e.cached
		e.mu.RUnlock()
		return v
	}
	e.mu.RUnlock()

	// Cache miss or expired — refresh from store.
	v, err := e.store.Get(context.Background())
	if err != nil {
		e.logger.Warn("vector: store read failed, using cached/default vector",
			zap.Error(err),
		)
		e.mu.RLock()
		defer e.mu.RUnlock()
		if e.cacheLoaded {
			return e.cached
		}
		return DefaultVector()
	}

	e.mu.Lock()
	e.cached = v
	e.cachedAt = time.Now()
	e.cacheLoaded = true
	e.mu.Unlock()

	return v
}
