package causal

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- Rule 1: Policy improvement → internal attribution ---

func TestAnalyze_PolicyImprovement_Internal(t *testing.T) {
	improved := true
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "feedbackAvoidPenalty",
				OldValue:            0.40,
				NewValue:            0.45,
				Applied:             true,
				CreatedAt:           time.Now().Add(-15 * time.Minute),
				ImprovementDetected: &improved,
			},
		},
		StabilityMode:       "normal",
		ProviderInstability: false,
		CycleInstability:    false,
		HighSystemFailure:   false,
		SimultaneousChanges: 1,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectPolicyChange && a.Attribution == AttributionInternal {
			found = true
			if a.Confidence < 0.70 {
				t.Errorf("expected confidence >= 0.70, got %f", a.Confidence)
			}
			if len(a.CompetingExplanations) == 0 {
				t.Error("expected competing explanations to be populated")
			}
		}
	}
	if !found {
		t.Errorf("expected internal attribution for policy improvement, got: %+v", attributions)
	}
}

// --- Rule 2: Metric change with external instability → external/mixed ---

func TestAnalyze_PolicyWithExternalInstability_Mixed(t *testing.T) {
	improved := true
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "feedbackPreferBoost",
				OldValue:            0.25,
				NewValue:            0.28,
				Applied:             true,
				CreatedAt:           time.Now().Add(-10 * time.Minute),
				ImprovementDetected: &improved,
			},
		},
		StabilityMode:       "normal",
		ProviderInstability: true,
		CycleInstability:    false,
		HighSystemFailure:   true,
		SimultaneousChanges: 1,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectPolicyChange && a.Attribution == AttributionMixed {
			found = true
			if a.Confidence >= 0.70 {
				t.Errorf("expected confidence < 0.70 for mixed, got %f", a.Confidence)
			}
		}
	}
	if !found {
		t.Errorf("expected mixed attribution, got: %+v", attributions)
	}
}

func TestAnalyze_PolicyNoImprovement_ExternalCause(t *testing.T) {
	notImproved := false
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "highRetryBoost",
				OldValue:            0.15,
				NewValue:            0.10,
				Applied:             true,
				CreatedAt:           time.Now().Add(-10 * time.Minute),
				ImprovementDetected: &notImproved,
			},
		},
		StabilityMode:       "throttled",
		ProviderInstability: true,
		HighSystemFailure:   true,
		SimultaneousChanges: 1,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectPolicyChange && a.Attribution == AttributionExternal {
			found = true
		}
	}
	if !found {
		t.Errorf("expected external attribution, got: %+v", attributions)
	}
}

// --- Rule 3: Insufficient evidence → ambiguous ---

func TestAnalyze_InsufficientEvidence_Ambiguous(t *testing.T) {
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "feedbackAvoidPenalty",
				OldValue:            0.40,
				NewValue:            0.45,
				Applied:             true,
				CreatedAt:           time.Now().Add(-5 * time.Minute),
				ImprovementDetected: nil, // not yet evaluated
			},
		},
		StabilityMode:       "normal",
		SimultaneousChanges: 1,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectPolicyChange && a.Attribution == AttributionAmbiguous {
			found = true
			if a.Confidence > 0.30 {
				t.Errorf("expected low confidence for unevaluated, got %f", a.Confidence)
			}
		}
	}
	if !found {
		t.Errorf("expected ambiguous attribution for unevaluated change, got: %+v", attributions)
	}
}

func TestAnalyze_TooManySimultaneousChanges_Ambiguous(t *testing.T) {
	improved := true
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "feedbackAvoidPenalty",
				OldValue:            0.40,
				NewValue:            0.45,
				Applied:             true,
				CreatedAt:           time.Now().Add(-10 * time.Minute),
				ImprovementDetected: &improved,
			},
		},
		StabilityMode:       "normal",
		SimultaneousChanges: 4,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectPolicyChange && a.Attribution == AttributionAmbiguous {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ambiguous attribution for many simultaneous changes, got: %+v", attributions)
	}
}

// --- Rule 4: Stability intervention reduces loops → internal ---

func TestAnalyze_StabilityEscalation_Effective(t *testing.T) {
	input := AnalysisInput{
		StabilityMode:       "throttled",
		StabilityChanged:    true,
		PreviousMode:        "normal",
		HighSystemFailure:   false,
		CycleInstability:    false,
		ProviderInstability: false,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectStabilityEvent && a.Attribution == AttributionInternal {
			found = true
			if a.Confidence < 0.60 {
				t.Errorf("expected confidence >= 0.60, got %f", a.Confidence)
			}
		}
	}
	if !found {
		t.Errorf("expected internal attribution for effective stability, got: %+v", attributions)
	}
}

func TestAnalyze_StabilityEscalation_StillUnstable(t *testing.T) {
	input := AnalysisInput{
		StabilityMode:     "safe_mode",
		StabilityChanged:  true,
		PreviousMode:      "throttled",
		HighSystemFailure: true,
		CycleInstability:  true,
		Timestamp:         time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectStabilityEvent && a.Attribution == AttributionMixed {
			found = true
		}
	}
	if !found {
		t.Errorf("expected mixed attribution for ongoing instability, got: %+v", attributions)
	}
}

func TestAnalyze_StabilityDeescalation(t *testing.T) {
	input := AnalysisInput{
		StabilityMode:    "normal",
		StabilityChanged: true,
		PreviousMode:     "throttled",
		Timestamp:        time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectStabilityEvent && a.Attribution == AttributionInternal {
			found = true
		}
	}
	if !found {
		t.Errorf("expected internal attribution for de-escalation, got: %+v", attributions)
	}
}

// --- Rule 5: No causal support ---

func TestAnalyze_PolicyNoImprovement_NoCause(t *testing.T) {
	notImproved := false
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "noopBasePenalty",
				OldValue:            0.20,
				NewValue:            0.25,
				Applied:             true,
				CreatedAt:           time.Now().Add(-15 * time.Minute),
				ImprovementDetected: &notImproved,
			},
		},
		StabilityMode:       "normal",
		ProviderInstability: false,
		HighSystemFailure:   false,
		SimultaneousChanges: 1,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectPolicyChange && a.Attribution == AttributionAmbiguous {
			found = true
			if a.Confidence > 0.40 {
				t.Errorf("expected low confidence for no-improvement ambiguous, got %f", a.Confidence)
			}
		}
	}
	if !found {
		t.Errorf("expected ambiguous attribution for no improvement, got: %+v", attributions)
	}
}

// --- Competing explanations always recorded ---

func TestAnalyze_CompetingExplanationsAlwaysPopulated(t *testing.T) {
	improved := true
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "feedbackAvoidPenalty",
				OldValue:            0.40,
				NewValue:            0.45,
				Applied:             true,
				CreatedAt:           time.Now().Add(-15 * time.Minute),
				ImprovementDetected: &improved,
			},
		},
		StabilityMode:       "normal",
		SimultaneousChanges: 1,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	for _, a := range attributions {
		if len(a.CompetingExplanations) == 0 {
			t.Errorf("attribution %s/%s has no competing explanations", a.SubjectType, a.Attribution)
		}
	}
}

// --- Planner degradation from external conditions ---

func TestAnalyze_PlannerDegradation_External(t *testing.T) {
	input := AnalysisInput{
		StabilityMode:       "normal",
		ProviderInstability: true,
		HighSystemFailure:   true,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	found := false
	for _, a := range attributions {
		if a.SubjectType == SubjectPlannerShift && a.Attribution == AttributionExternal {
			found = true
		}
	}
	if !found {
		t.Errorf("expected external planner attribution, got: %+v", attributions)
	}
}

// --- No attributions when nothing to analyze ---

func TestAnalyze_NoSignals_Empty(t *testing.T) {
	input := AnalysisInput{
		StabilityMode: "normal",
		Timestamp:     time.Now(),
	}

	attributions := Analyze(input)

	if len(attributions) != 0 {
		t.Errorf("expected 0 attributions with no signals, got %d", len(attributions))
	}
}

// --- Non-applied policy changes are skipped ---

func TestAnalyze_NonAppliedChangesSkipped(t *testing.T) {
	improved := true
	input := AnalysisInput{
		RecentPolicyChanges: []PolicyChangeRecord{
			{
				ID:                  uuid.New(),
				Parameter:           "feedbackAvoidPenalty",
				OldValue:            0.40,
				NewValue:            0.45,
				Applied:             false, // not applied
				CreatedAt:           time.Now().Add(-10 * time.Minute),
				ImprovementDetected: &improved,
			},
		},
		StabilityMode:       "normal",
		SimultaneousChanges: 1,
		Timestamp:           time.Now(),
	}

	attributions := Analyze(input)

	for _, a := range attributions {
		if a.SubjectType == SubjectPolicyChange {
			t.Error("should not produce attribution for non-applied policy change")
		}
	}
}
