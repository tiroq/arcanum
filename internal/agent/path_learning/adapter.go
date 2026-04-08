package pathlearning

import (
	"context"

	"go.uber.org/zap"
)

// PathLearningProvider is the interface used by the decision graph layer
// to retrieve path and transition feedback without importing this package directly.
type PathLearningProvider interface {
	// GetPathFeedback returns path feedback for a given signature + goal type.
	// Returns neutral recommendation if data is unavailable or insufficient.
	GetPathFeedback(ctx context.Context, pathSignature, goalType string) PathFeedback

	// GetTransitionFeedback returns transition feedback for a given key + goal type.
	// Returns neutral recommendation if data is unavailable or insufficient.
	GetTransitionFeedback(ctx context.Context, transitionKey, goalType string) TransitionFeedback

	// GetAllPathFeedback returns path feedback for all known paths of a goal type.
	GetAllPathFeedback(ctx context.Context, goalType string) map[string]PathFeedback

	// GetAllTransitionFeedback returns transition feedback for all known transitions of a goal type.
	GetAllTransitionFeedback(ctx context.Context, goalType string) map[string]TransitionFeedback
}

// GraphAdapter adapts the path learning layer to the decision graph.
// Implements PathLearningProvider for use by the graph planner adapter.
type GraphAdapter struct {
	memoryStore     *MemoryStore
	transitionStore *TransitionStore
	logger          *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter.
func NewGraphAdapter(
	memoryStore *MemoryStore,
	transitionStore *TransitionStore,
	logger *zap.Logger,
) *GraphAdapter {
	return &GraphAdapter{
		memoryStore:     memoryStore,
		transitionStore: transitionStore,
		logger:          logger,
	}
}

// GetPathFeedback returns path feedback for a given signature + goal type.
// Fail-open: returns neutral if data is unavailable.
func (a *GraphAdapter) GetPathFeedback(ctx context.Context, pathSignature, goalType string) PathFeedback {
	if a.memoryStore == nil {
		return neutralPathFeedback(pathSignature, goalType)
	}

	record, err := a.memoryStore.GetPathMemory(ctx, pathSignature, goalType)
	if err != nil {
		a.logger.Warn("path_feedback_query_failed",
			zap.String("path_signature", pathSignature),
			zap.Error(err),
		)
		return neutralPathFeedback(pathSignature, goalType)
	}
	if record == nil {
		return neutralPathFeedback(pathSignature, goalType)
	}

	return GeneratePathFeedback(record)
}

// GetTransitionFeedback returns transition feedback for a given key + goal type.
// Fail-open: returns neutral if data is unavailable.
func (a *GraphAdapter) GetTransitionFeedback(ctx context.Context, transitionKey, goalType string) TransitionFeedback {
	if a.transitionStore == nil {
		return neutralTransitionFeedback(transitionKey, goalType)
	}

	record, err := a.transitionStore.GetTransitionMemory(ctx, goalType, transitionKey)
	if err != nil {
		a.logger.Warn("transition_feedback_query_failed",
			zap.String("transition_key", transitionKey),
			zap.Error(err),
		)
		return neutralTransitionFeedback(transitionKey, goalType)
	}
	if record == nil {
		return neutralTransitionFeedback(transitionKey, goalType)
	}

	return GenerateTransitionFeedback(record)
}

// GetAllPathFeedback returns path feedback for all known paths of a goal type.
// Fail-open: returns empty map if data is unavailable.
func (a *GraphAdapter) GetAllPathFeedback(ctx context.Context, goalType string) map[string]PathFeedback {
	result := make(map[string]PathFeedback)

	if a.memoryStore == nil {
		return result
	}

	records, err := a.memoryStore.ListPathMemoryByGoalType(ctx, goalType)
	if err != nil {
		a.logger.Warn("path_feedback_list_failed",
			zap.String("goal_type", goalType),
			zap.Error(err),
		)
		return result
	}

	for i := range records {
		fb := GeneratePathFeedback(&records[i])
		result[records[i].PathSignature] = fb
	}

	return result
}

// GetAllTransitionFeedback returns transition feedback for all known transitions of a goal type.
// Fail-open: returns empty map if data is unavailable.
func (a *GraphAdapter) GetAllTransitionFeedback(ctx context.Context, goalType string) map[string]TransitionFeedback {
	result := make(map[string]TransitionFeedback)

	if a.transitionStore == nil {
		return result
	}

	records, err := a.transitionStore.ListTransitionMemoryByGoalType(ctx, goalType)
	if err != nil {
		a.logger.Warn("transition_feedback_list_failed",
			zap.String("goal_type", goalType),
			zap.Error(err),
		)
		return result
	}

	for i := range records {
		fb := GenerateTransitionFeedback(&records[i])
		result[records[i].TransitionKey] = fb
	}

	return result
}

// --- Neutral fallbacks ---

func neutralPathFeedback(pathSignature, goalType string) PathFeedback {
	return PathFeedback{
		PathSignature:  pathSignature,
		GoalType:       goalType,
		Recommendation: RecommendNeutralPath,
	}
}

func neutralTransitionFeedback(transitionKey, goalType string) TransitionFeedback {
	return TransitionFeedback{
		TransitionKey:  transitionKey,
		GoalType:       goalType,
		Recommendation: RecommendNeutralTransition,
	}
}

// --- Decision Graph PathLearningProvider bridge ---
// These methods satisfy decision_graph.PathLearningProvider, returning
// map[string]string for use in graph scoring adjustments.

// GetAllPathFeedbackMap returns path feedback as a map[pathSignature] → recommendation string.
// Satisfies decision_graph.PathLearningProvider.
func (a *GraphAdapter) GetAllPathFeedbackMap(ctx context.Context, goalType string) map[string]string {
	result := make(map[string]string)

	feedbacks := a.GetAllPathFeedback(ctx, goalType)
	for sig, fb := range feedbacks {
		result[sig] = string(fb.Recommendation)
	}

	return result
}

// GetAllTransitionFeedbackMap returns transition feedback as a map[transitionKey] → recommendation string.
// Satisfies decision_graph.PathLearningProvider.
func (a *GraphAdapter) GetAllTransitionFeedbackMap(ctx context.Context, goalType string) map[string]string {
	result := make(map[string]string)

	feedbacks := a.GetAllTransitionFeedback(ctx, goalType)
	for key, fb := range feedbacks {
		result[key] = string(fb.Recommendation)
	}

	return result
}
