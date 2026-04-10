package discovery

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates the opportunity discovery pipeline:
// gather inputs → evaluate rules → dedupe → persist candidates → audit.
// Fail-open: all errors are logged but do not stop the pipeline.
type Engine struct {
	store        *CandidateStore
	deduplicator *Deduplicator
	promoter     *Promoter
	rules        []Rule
	signals      SignalProvider
	outcomes     OutcomeProvider
	proposals    ProposalProvider
	auditor      audit.AuditRecorder
	logger       *zap.Logger
}

// NewEngine creates a discovery Engine with required dependencies.
func NewEngine(
	store *CandidateStore,
	deduplicator *Deduplicator,
	promoter *Promoter,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		store:        store,
		deduplicator: deduplicator,
		promoter:     promoter,
		rules:        DefaultRules(),
		auditor:      auditor,
		logger:       logger,
	}
}

// WithSignals attaches a signal provider.
func (e *Engine) WithSignals(sp SignalProvider) *Engine {
	e.signals = sp
	return e
}

// WithOutcomes attaches an outcome provider.
func (e *Engine) WithOutcomes(op OutcomeProvider) *Engine {
	e.outcomes = op
	return e
}

// WithProposals attaches a proposal provider.
func (e *Engine) WithProposals(pp ProposalProvider) *Engine {
	e.proposals = pp
	return e
}

// WithRules replaces the default rules (primarily for testing).
func (e *Engine) WithRules(rules []Rule) *Engine {
	e.rules = rules
	return e
}

// Run executes a full discovery pass:
// 1. Gather inputs from providers
// 2. Evaluate all rules
// 3. Dedupe candidates
// 4. Persist new candidates
// 5. Emit audit events
func (e *Engine) Run(ctx context.Context) DiscoveryRunResult {
	result := DiscoveryRunResult{}

	// 1. Gather inputs.
	input := e.gatherInputs(ctx)

	// 2. Evaluate rules.
	for _, rule := range e.rules {
		ruleResult, candidates := rule.Evaluate(ctx, input)
		result.RuleResults = append(result.RuleResults, ruleResult)

		if !ruleResult.Matched {
			continue
		}

		// 3. Dedupe + persist.
		for _, candidate := range candidates {
			dedupeResult := DedupeResult{IsDuplicate: false, Action: "created"}
			if e.deduplicator != nil {
				dedupeResult = e.deduplicator.Check(ctx, candidate)
			}

			if dedupeResult.IsDuplicate {
				result.CandidatesDeduped++
				e.auditEvent(ctx, "income.discovery_candidate_deduped", map[string]any{
					"rule_name":      rule.Name(),
					"candidate_type": candidate.CandidateType,
					"dedupe_key":     candidate.DedupeKey,
					"evidence_count": candidate.EvidenceCount,
					"action":         dedupeResult.Action,
					"existing_id":    dedupeResult.ExistingID,
				})
				continue
			}

			// 4. Persist.
			if e.store != nil {
				saved, err := e.store.Create(ctx, candidate)
				if err != nil {
					e.logger.Warn("failed to persist discovery candidate",
						zap.Error(err),
						zap.String("dedupe_key", candidate.DedupeKey),
					)
					continue
				}
				candidate = saved
			}

			result.CandidatesCreated++
			e.auditEvent(ctx, "income.discovery_candidate_created", map[string]any{
				"rule_name":       rule.Name(),
				"candidate_id":    candidate.ID,
				"candidate_type":  candidate.CandidateType,
				"dedupe_key":      candidate.DedupeKey,
				"evidence_count":  candidate.EvidenceCount,
				"confidence":      candidate.Confidence,
				"estimated_value": candidate.EstimatedValue,
			})
		}
	}

	// 5. Audit the run.
	e.auditEvent(ctx, "income.discovery_run", map[string]any{
		"rules_evaluated":    len(e.rules),
		"candidates_created": result.CandidatesCreated,
		"candidates_deduped": result.CandidatesDeduped,
	})

	e.logger.Info("discovery run completed",
		zap.Int("rules", len(e.rules)),
		zap.Int("created", result.CandidatesCreated),
		zap.Int("deduped", result.CandidatesDeduped),
	)

	return result
}

// Promote promotes a candidate to an income opportunity.
func (e *Engine) Promote(ctx context.Context, candidateID string) error {
	if e.store == nil {
		return nil
	}

	candidate, err := e.store.GetByID(ctx, candidateID)
	if err != nil {
		return err
	}

	if e.promoter == nil {
		return nil
	}

	opp, err := e.promoter.Promote(ctx, candidate)
	if err != nil {
		e.logger.Warn("failed to promote candidate",
			zap.String("candidate_id", candidateID),
			zap.Error(err),
		)
		return err
	}

	// Mark candidate as promoted.
	_ = e.store.UpdateStatus(ctx, candidateID, CandidateStatusPromoted)

	e.auditEvent(ctx, "income.discovery_candidate_promoted", map[string]any{
		"candidate_id":     candidateID,
		"candidate_type":   candidate.CandidateType,
		"dedupe_key":       candidate.DedupeKey,
		"evidence_count":   candidate.EvidenceCount,
		"opportunity_id":   opp.ID,
		"opportunity_type": opp.OpportunityType,
	})

	e.logger.Info("discovery candidate promoted to income opportunity",
		zap.String("candidate_id", candidateID),
		zap.String("opportunity_id", opp.ID),
		zap.String("opportunity_type", opp.OpportunityType),
	)

	return nil
}

// ListCandidates returns paginated candidates.
func (e *Engine) ListCandidates(ctx context.Context, limit, offset int) ([]DiscoveryCandidate, error) {
	if e.store == nil {
		return []DiscoveryCandidate{}, nil
	}
	return e.store.List(ctx, limit, offset)
}

// Stats returns aggregate discovery statistics.
func (e *Engine) Stats(ctx context.Context) (DiscoveryStats, error) {
	if e.store == nil {
		return DiscoveryStats{}, nil
	}
	stats, err := e.store.Stats(ctx)
	if err != nil {
		return DiscoveryStats{}, err
	}
	stats.TotalCandidatesDeduped = e.store.CountDeduped(ctx)
	return stats, nil
}

// gatherInputs collects data from all providers.
func (e *Engine) gatherInputs(ctx context.Context) DiscoveryInput {
	var input DiscoveryInput

	windowHours := DefaultDiscoveryWindowHours

	if e.signals != nil {
		sigs, err := e.signals.ListRecentSignals(ctx, windowHours, 500)
		if err != nil {
			e.logger.Warn("failed to gather signals for discovery", zap.Error(err))
		} else {
			input.Signals = sigs
		}
	}

	if e.outcomes != nil {
		outcomes, err := e.outcomes.ListRecentOutcomes(ctx, windowHours, 500)
		if err != nil {
			e.logger.Warn("failed to gather outcomes for discovery", zap.Error(err))
		} else {
			input.Outcomes = outcomes
		}
	}

	if e.proposals != nil {
		proposals, err := e.proposals.ListRecentProposals(ctx, windowHours, 500)
		if err != nil {
			e.logger.Warn("failed to gather proposals for discovery", zap.Error(err))
		} else {
			input.Proposals = proposals
		}
	}

	return input
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "discovery", uuid.New(), eventType,
		"system", "discovery_engine", payload)
}
