package calibration

import "time"

// CalibrationContext represents the context dimensions for context-aware calibration.
type CalibrationContext struct {
	GoalType     string `json:"goal_type"`
	ProviderName string `json:"provider_name,omitempty"`
	StrategyType string `json:"strategy_type,omitempty"`
}

// CalibrationContextRecord stores the per-context calibration data.
type CalibrationContextRecord struct {
	ID                     int       `json:"id"`
	GoalType               *string   `json:"goal_type"`
	ProviderName           *string   `json:"provider_name"`
	StrategyType           *string   `json:"strategy_type"`
	SampleCount            int       `json:"sample_count"`
	AvgPredictedConfidence float64   `json:"avg_predicted_confidence"`
	AvgActualSuccess       float64   `json:"avg_actual_success"`
	CalibrationError       float64   `json:"calibration_error"`
	LastUpdated            time.Time `json:"last_updated"`
}

// Context resolution levels.
const (
	ContextLevelL0     = "L0" // goal + provider + strategy (exact)
	ContextLevelL1     = "L1" // goal + strategy
	ContextLevelL2     = "L2" // goal only
	ContextLevelL3     = "L3" // global
	ContextLevelNone   = ""   // no match
)

// Contextual calibration constants.
const (
	// ContextMinSamples is the minimum sample count for a context record to be used.
	ContextMinSamples = 5

	// ContextMaxAdjustment is the maximum absolute adjustment magnitude.
	ContextMaxAdjustment = 0.20
)

// contextKeys returns all resolution levels for a given context, from most specific to least.
// L0: goal + provider + strategy
// L1: goal + strategy
// L2: goal only
// L3: global (all nil)
func contextKeys(ctx CalibrationContext) []CalibrationContext {
	keys := make([]CalibrationContext, 0, 4)

	// L0: full match (only if all dimensions present)
	if ctx.GoalType != "" && ctx.ProviderName != "" && ctx.StrategyType != "" {
		keys = append(keys, CalibrationContext{
			GoalType:     ctx.GoalType,
			ProviderName: ctx.ProviderName,
			StrategyType: ctx.StrategyType,
		})
	}

	// L1: goal + strategy (only if both present)
	if ctx.GoalType != "" && ctx.StrategyType != "" {
		keys = append(keys, CalibrationContext{
			GoalType:     ctx.GoalType,
			StrategyType: ctx.StrategyType,
		})
	}

	// L2: goal only
	if ctx.GoalType != "" {
		keys = append(keys, CalibrationContext{
			GoalType: ctx.GoalType,
		})
	}

	// L3: global
	keys = append(keys, CalibrationContext{})

	return keys
}
