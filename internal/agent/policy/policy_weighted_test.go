package policy

import (
	"testing"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// TestProposal_EffectivePattern_ConfidenceScaledByRecency verifies that
// the effective pattern rule reduces proposal confidence when evidence is stale.
func TestProposal_EffectivePattern_ConfidenceScaledByRecency(t *testing.T) {
	now := time.Now().UTC()

	freshInput := ProposalInput{
		ActionMemory: []actionmemory.ActionMemoryRecord{{
			ActionType:  "retry_job",
			TotalRuns:   10,
			SuccessRuns: 8,
			SuccessRate: 0.80,
			FailureRate: 0.20,
			LastUpdated: now.Add(-30 * time.Minute),
		}},
		CurrentValues: map[PolicyParam]float64{ParamFeedbackPreferBoost: 0.25},
		StabilityMode: "normal",
		Now:           now,
	}

	staleInput := ProposalInput{
		ActionMemory: []actionmemory.ActionMemoryRecord{{
			ActionType:  "retry_job",
			TotalRuns:   10,
			SuccessRuns: 8,
			SuccessRate: 0.80,
			FailureRate: 0.20,
			LastUpdated: now.Add(-14 * 24 * time.Hour),
		}},
		CurrentValues: map[PolicyParam]float64{ParamFeedbackPreferBoost: 0.25},
		StabilityMode: "normal",
		Now:           now,
	}

	freshProposals := ruleEffectivePattern(freshInput)
	staleProposals := ruleEffectivePattern(staleInput)

	if len(freshProposals) == 0 || len(staleProposals) == 0 {
		t.Fatal("expected proposals from both inputs")
	}

	if staleProposals[0].Confidence >= freshProposals[0].Confidence {
		t.Errorf("stale confidence (%.3f) should be less than fresh (%.3f)",
			staleProposals[0].Confidence, freshProposals[0].Confidence)
	}
}

// TestProposal_RetryAmplification_ConfidenceScaledByRecency verifies that
// the retry amplification rule reduces confidence when evidence is stale.
func TestProposal_RetryAmplification_ConfidenceScaledByRecency(t *testing.T) {
	now := time.Now().UTC()

	freshInput := ProposalInput{
		ActionMemory: []actionmemory.ActionMemoryRecord{{
			ActionType:  "retry_job",
			TotalRuns:   10,
			SuccessRuns: 3,
			SuccessRate: 0.30,
			FailureRuns: 5,
			FailureRate: 0.50,
			LastUpdated: now.Add(-1 * time.Hour),
		}},
		CurrentValues: map[PolicyParam]float64{ParamHighRetryBoost: 0.15},
		StabilityMode: "normal",
		Now:           now,
	}

	staleInput := ProposalInput{
		ActionMemory: []actionmemory.ActionMemoryRecord{{
			ActionType:  "retry_job",
			TotalRuns:   10,
			SuccessRuns: 3,
			SuccessRate: 0.30,
			FailureRuns: 5,
			FailureRate: 0.50,
			LastUpdated: now.Add(-14 * 24 * time.Hour),
		}},
		CurrentValues: map[PolicyParam]float64{ParamHighRetryBoost: 0.15},
		StabilityMode: "normal",
		Now:           now,
	}

	freshProposals := ruleRetryAmplification(freshInput)
	staleProposals := ruleRetryAmplification(staleInput)

	if len(freshProposals) == 0 || len(staleProposals) == 0 {
		t.Fatal("expected proposals from both inputs")
	}

	if staleProposals[0].Confidence >= freshProposals[0].Confidence {
		t.Errorf("stale confidence (%.3f) should be less than fresh (%.3f)",
			staleProposals[0].Confidence, freshProposals[0].Confidence)
	}
}

// TestProposal_EffectivePattern_NoTimestamp_DefaultConfidence verifies
// backward compatibility: when Now is zero, base confidence is used.
func TestProposal_EffectivePattern_NoTimestamp_DefaultConfidence(t *testing.T) {
	input := ProposalInput{
		ActionMemory: []actionmemory.ActionMemoryRecord{{
			ActionType:  "retry_job",
			TotalRuns:   10,
			SuccessRuns: 8,
			SuccessRate: 0.80,
			FailureRate: 0.20,
		}},
		CurrentValues: map[PolicyParam]float64{ParamFeedbackPreferBoost: 0.25},
		StabilityMode: "normal",
		// Now is zero — backward compat
	}

	proposals := ruleEffectivePattern(input)
	if len(proposals) == 0 {
		t.Fatal("expected proposal")
	}
	if proposals[0].Confidence != 0.75 {
		t.Errorf("expected default confidence 0.75, got %.3f", proposals[0].Confidence)
	}
}
