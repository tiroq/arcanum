package vector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultVector(t *testing.T) {
	v := DefaultVector()
	assert.Equal(t, 0.70, v.IncomePriority)
	assert.Equal(t, 1.00, v.FamilySafetyPriority)
	assert.Equal(t, 0.30, v.InfraPriority)
	assert.Equal(t, 0.40, v.AutomationPriority)
	assert.Equal(t, 0.30, v.ExplorationLevel)
	assert.Equal(t, 0.30, v.RiskTolerance)
	assert.Equal(t, 0.80, v.HumanReviewStrictness)
}

func TestClamp(t *testing.T) {
	v := SystemVector{
		IncomePriority:        1.5,
		FamilySafetyPriority:  -0.3,
		InfraPriority:         0.5,
		AutomationPriority:    2.0,
		ExplorationLevel:      -1.0,
		RiskTolerance:         0.5,
		HumanReviewStrictness: 0.9,
	}
	v.Clamp()
	assert.Equal(t, 1.0, v.IncomePriority)
	assert.Equal(t, 0.0, v.FamilySafetyPriority)
	assert.Equal(t, 0.5, v.InfraPriority)
	assert.Equal(t, 1.0, v.AutomationPriority)
	assert.Equal(t, 0.0, v.ExplorationLevel)
}

func TestInMemoryStore(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()

	v, err := store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0.70, v.IncomePriority)

	v.IncomePriority = 0.90
	v.RiskTolerance = 0.50
	require.NoError(t, store.Set(ctx, v))

	got, err := store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0.90, got.IncomePriority)
	assert.Equal(t, 0.50, got.RiskTolerance)
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestEngine(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()
	logger := zap.NewNop()
	engine := NewEngine(store, nil, logger)

	v, err := engine.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, DefaultVector().IncomePriority, v.IncomePriority)

	v.IncomePriority = 0.95
	require.NoError(t, engine.Set(ctx, v))

	got, err := engine.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0.95, got.IncomePriority)
}

func TestGetVectorProvider(t *testing.T) {
	store := NewInMemoryStore()
	logger := zap.NewNop()
	engine := NewEngine(store, nil, logger)

	v := engine.GetVector()
	assert.Equal(t, DefaultVector().IncomePriority, v.IncomePriority)
}

func TestGraphAdapterNilSafe(t *testing.T) {
	var a *GraphAdapter
	v := a.GetVector()
	assert.Equal(t, DefaultVector().IncomePriority, v.IncomePriority)
	assert.Equal(t, DefaultVector().RiskTolerance, a.GetRiskTolerance())
}

func TestGraphAdapter(t *testing.T) {
	store := NewInMemoryStore()
	logger := zap.NewNop()
	engine := NewEngine(store, nil, logger)
	adapter := NewGraphAdapter(engine)

	assert.Equal(t, 0.70, adapter.GetIncomePriority())
	assert.Equal(t, 1.00, adapter.GetFamilySafetyPriority())
	assert.Equal(t, 0.30, adapter.GetRiskTolerance())
	assert.Equal(t, 0.80, adapter.GetHumanReviewStrictness())
	assert.Equal(t, 0.30, adapter.GetExplorationLevel())
	assert.Equal(t, 0.40, adapter.GetAutomationPriority())
}
