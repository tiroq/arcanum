package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// --- Mock providers ---

type mockSignalProvider struct {
	signals []SignalRecord
	err     error
}

func (m *mockSignalProvider) ListRecentSignals(_ context.Context, _ int, _ int) ([]SignalRecord, error) {
	return m.signals, m.err
}

type mockOutcomeProvider struct {
	outcomes []OutcomeRecord
	err      error
}

func (m *mockOutcomeProvider) ListRecentOutcomes(_ context.Context, _ int, _ int) ([]OutcomeRecord, error) {
	return m.outcomes, m.err
}

type mockProposalProvider struct {
	proposals []ProposalRecord
	err       error
}

func (m *mockProposalProvider) ListRecentProposals(_ context.Context, _ int, _ int) ([]ProposalRecord, error) {
	return m.proposals, m.err
}

type mockOpportunityProvider struct {
	hasActive bool
	err       error
}

func (m *mockOpportunityProvider) HasActiveOpportunity(_ context.Context, _, _ string) (bool, error) {
	return m.hasActive, m.err
}

// --- Mock audit recorder ---

type mockAuditRecorder struct {
	events []auditEvent
}

type auditEvent struct {
	eventType string
	payload   any
}

func (m *mockAuditRecorder) RecordEvent(_ context.Context, _ string, _ uuid.UUID, eventType, _, _ string, payload any) error {
	m.events = append(m.events, auditEvent{eventType: eventType, payload: payload})
	return nil
}

// ---------------------------------------------------------------
// Rule Tests (11.1)
// ---------------------------------------------------------------

// Test 1: Repeated manual work → automation candidate
func TestRule_RepeatedManualWork(t *testing.T) {
	rule := NewRepeatedManualWorkRule(3)
	input := DiscoveryInput{
		Outcomes: []OutcomeRecord{
			{ActionType: "summarize_state", Status: "succeeded", GoalType: "system_reliability"},
			{ActionType: "summarize_state", Status: "succeeded", GoalType: "system_reliability"},
			{ActionType: "summarize_state", Status: "succeeded", GoalType: "system_reliability"},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if !result.Matched {
		t.Fatal("expected rule to match")
	}
	if result.RuleName != "repeated_manual_work" {
		t.Errorf("expected rule_name=repeated_manual_work, got %s", result.RuleName)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].CandidateType != CandidateAutomation {
		t.Errorf("expected candidate_type=%s, got %s", CandidateAutomation, candidates[0].CandidateType)
	}
	if candidates[0].EvidenceCount != 3 {
		t.Errorf("expected evidence_count=3, got %d", candidates[0].EvidenceCount)
	}
}

// Test: Repeated manual work below threshold → no match
func TestRule_RepeatedManualWork_BelowThreshold(t *testing.T) {
	rule := NewRepeatedManualWorkRule(3)
	input := DiscoveryInput{
		Outcomes: []OutcomeRecord{
			{ActionType: "summarize_state", Status: "succeeded"},
			{ActionType: "summarize_state", Status: "succeeded"},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if result.Matched {
		t.Error("expected rule not to match with only 2 occurrences")
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

// Test 2: Repeated solved issue → reusable candidate
func TestRule_RepeatedSolvedIssue(t *testing.T) {
	rule := NewRepeatedSolvedIssueRule(3)
	input := DiscoveryInput{
		Outcomes: []OutcomeRecord{
			{ActionType: "fix_bug", Status: "succeeded", GoalType: "system_reliability"},
			{ActionType: "fix_bug", Status: "succeeded", GoalType: "system_reliability"},
			{ActionType: "fix_bug", Status: "succeeded", GoalType: "system_reliability"},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if !result.Matched {
		t.Fatal("expected rule to match")
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].CandidateType != CandidateResaleRepackage {
		t.Errorf("expected candidate_type=%s, got %s", CandidateResaleRepackage, candidates[0].CandidateType)
	}
}

// Test 3: Inbound request → consulting lead
func TestRule_InboundNeed(t *testing.T) {
	rule := NewInboundNeedRule(1)
	input := DiscoveryInput{
		Signals: []SignalRecord{
			{SignalType: "new_opportunity", Source: "external", ObservedAt: time.Now()},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if !result.Matched {
		t.Fatal("expected rule to match")
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate")
	}
	if candidates[0].CandidateType != CandidateConsultingLead {
		t.Errorf("expected candidate_type=%s, got %s", CandidateConsultingLead, candidates[0].CandidateType)
	}
}

// Test: Inbound need with manual source
func TestRule_InboundNeed_ManualSource(t *testing.T) {
	rule := NewInboundNeedRule(1)
	input := DiscoveryInput{
		Signals: []SignalRecord{
			{SignalType: "pending_tasks", Source: "manual", ObservedAt: time.Now()},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if !result.Matched {
		t.Fatal("expected rule to match with manual source")
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate")
	}
}

// Test 4: Repeated cost spike → cost saving candidate
func TestRule_CostWaste(t *testing.T) {
	rule := NewCostWasteRule(2)
	input := DiscoveryInput{
		Signals: []SignalRecord{
			{SignalType: "cost_spike", Source: "monitoring", ObservedAt: time.Now()},
			{SignalType: "cost_spike", Source: "monitoring", ObservedAt: time.Now()},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if !result.Matched {
		t.Fatal("expected rule to match")
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].CandidateType != CandidateCostSaving {
		t.Errorf("expected candidate_type=%s, got %s", CandidateCostSaving, candidates[0].CandidateType)
	}
}

// Test: Cost waste below threshold
func TestRule_CostWaste_BelowThreshold(t *testing.T) {
	rule := NewCostWasteRule(2)
	input := DiscoveryInput{
		Signals: []SignalRecord{
			{SignalType: "cost_spike", Source: "monitoring", ObservedAt: time.Now()},
		},
	}

	result, _ := rule.Evaluate(context.Background(), input)

	if result.Matched {
		t.Error("expected rule not to match with only 1 cost spike")
	}
}

// Test 5: Reusable internal success → product feature candidate
func TestRule_ReusableSuccess(t *testing.T) {
	rule := NewReusableSuccessRule(3)
	input := DiscoveryInput{
		Proposals: []ProposalRecord{
			{ActionType: "analyze_opportunity", OpportunityType: "automation", Status: "executed"},
			{ActionType: "analyze_opportunity", OpportunityType: "automation", Status: "executed"},
			{ActionType: "analyze_opportunity", OpportunityType: "automation", Status: "executed"},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if !result.Matched {
		t.Fatal("expected rule to match")
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate")
	}
	foundProduct := false
	for _, c := range candidates {
		if c.CandidateType == CandidateProductFeature {
			foundProduct = true
		}
	}
	if !foundProduct {
		t.Error("expected at least one product_feature_candidate")
	}
}

// Test: Reusable success from repeated succeeded outcomes
func TestRule_ReusableSuccess_FromOutcomes(t *testing.T) {
	rule := NewReusableSuccessRule(3)
	input := DiscoveryInput{
		Outcomes: []OutcomeRecord{
			{ActionType: "deploy_service", Status: "succeeded", GoalType: "system_reliability"},
			{ActionType: "deploy_service", Status: "succeeded", GoalType: "system_reliability"},
			{ActionType: "deploy_service", Status: "succeeded", GoalType: "system_reliability"},
		},
	}

	result, candidates := rule.Evaluate(context.Background(), input)

	if !result.Matched {
		t.Fatal("expected rule to match from outcomes")
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate from outcomes")
	}
}

// ---------------------------------------------------------------
// Deduplication Tests (11.2)
// ---------------------------------------------------------------

// Test 6: Same dedupe key not duplicated
func TestDedup_SameDedupKeyNotDuplicated(t *testing.T) {
	store := &memCandidateChecker{
		candidates: []DiscoveryCandidate{
			{
				ID:            "existing-1",
				CandidateType: CandidateAutomation,
				DedupeKey:     "manual_work:summarize_state",
				Status:        CandidateStatusNew,
				EvidenceCount: 1,
				CreatedAt:     time.Now(),
			},
		},
	}

	dedup := NewDeduplicator(store, nil, DedupeWindowHours)

	result := dedup.Check(context.Background(), DiscoveryCandidate{
		CandidateType: CandidateAutomation,
		DedupeKey:     "manual_work:summarize_state",
	})

	if !result.IsDuplicate {
		t.Error("expected duplicate to be detected")
	}
	if result.Action != "incremented" {
		t.Errorf("expected action=incremented, got %s", result.Action)
	}
	if result.ExistingID != "existing-1" {
		t.Errorf("expected existing_id=existing-1, got %s", result.ExistingID)
	}
}

// Test 7: Expired window allows recreation
func TestDedup_ExpiredWindowAllowsRecreation(t *testing.T) {
	store := &memCandidateChecker{
		candidates: []DiscoveryCandidate{
			{
				ID:            "old-1",
				CandidateType: CandidateAutomation,
				DedupeKey:     "manual_work:summarize_state",
				Status:        CandidateStatusNew,
				EvidenceCount: 1,
				CreatedAt:     time.Now().Add(-100 * time.Hour), // well beyond 72h window
			},
		},
		respectWindow: true,
	}

	dedup := NewDeduplicator(store, nil, DedupeWindowHours)

	result := dedup.Check(context.Background(), DiscoveryCandidate{
		CandidateType: CandidateAutomation,
		DedupeKey:     "manual_work:summarize_state",
	})

	if result.IsDuplicate {
		t.Error("expected no duplicate since candidate is outside the window")
	}
}

// Test 8: Active opportunity suppresses duplicate promotion
func TestDedup_ActiveOpportunitySuppresses(t *testing.T) {
	store := &memCandidateChecker{}

	dedup := NewDeduplicator(store, &mockOpportunityProvider{hasActive: true}, DedupeWindowHours)

	result := dedup.Check(context.Background(), DiscoveryCandidate{
		CandidateType: CandidateAutomation,
		DedupeKey:     "manual_work:summarize_state",
	})

	if !result.IsDuplicate {
		t.Error("expected duplicate when active opportunity exists")
	}
	if result.Action != "skipped" {
		t.Errorf("expected action=skipped, got %s", result.Action)
	}
}

// ---------------------------------------------------------------
// Promotion Tests (11.3)
// ---------------------------------------------------------------

// Test 9: Discovery candidate promotes to income opportunity (mock)
func TestPromotion_CandidateToOpportunityTypeMapping(t *testing.T) {
	tests := []struct {
		candidateType string
		oppType       string
	}{
		{CandidateAutomation, "automation"},
		{CandidateResaleRepackage, "service"},
		{CandidateConsultingLead, "consulting"},
		{CandidateCostSaving, "other"},
		{CandidateProductFeature, "service"},
	}

	for _, tt := range tests {
		got := CandidateToOpportunityType[tt.candidateType]
		if got != tt.oppType {
			t.Errorf("CandidateToOpportunityType[%s] = %s, want %s", tt.candidateType, got, tt.oppType)
		}
	}
}

// Test 10: Promoted candidate keeps expected fields
func TestPromotion_PreservesFields(t *testing.T) {
	candidate := DiscoveryCandidate{
		ID:              "test-1",
		CandidateType:   CandidateAutomation,
		SourceType:      SourceRepeatedManualWork,
		Title:           "Automate repeated manual work: summarize_state",
		Description:     "Test description",
		Confidence:      0.75,
		EstimatedValue:  500,
		EstimatedEffort: 0.4,
		DedupeKey:       "manual_work:summarize_state",
		EvidenceCount:   5,
	}

	oppType := CandidateToOpportunityType[candidate.CandidateType]
	if oppType != "automation" {
		t.Errorf("expected opportunity type 'automation', got %s", oppType)
	}
	if candidate.Title == "" {
		t.Error("title should be preserved")
	}
	if candidate.Confidence != 0.75 {
		t.Errorf("confidence should be preserved: got %f", candidate.Confidence)
	}
	if candidate.EstimatedValue != 500 {
		t.Errorf("estimated value should be preserved: got %f", candidate.EstimatedValue)
	}
}

// Test 11: Default rules include all 5 expected rules
func TestDefaultRules_AllPresent(t *testing.T) {
	rules := DefaultRules()
	if len(rules) != 5 {
		t.Fatalf("expected 5 default rules, got %d", len(rules))
	}

	names := map[string]bool{}
	for _, r := range rules {
		names[r.Name()] = true
	}

	expected := []string{
		"repeated_manual_work",
		"repeated_solved_issue",
		"inbound_need",
		"cost_waste",
		"reusable_success",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected rule %q not found in DefaultRules()", name)
		}
	}
}

// ---------------------------------------------------------------
// Integration Tests (11.4)
// ---------------------------------------------------------------

// Test 12: Full discovery run produces opportunities from signals
func TestEngine_RunProducesCandidates(t *testing.T) {
	auditor := &mockAuditRecorder{}
	logger := noopLogger()

	engine := &Engine{
		rules:   DefaultRules(),
		auditor: auditor,
		logger:  logger,
		signals: &mockSignalProvider{
			signals: []SignalRecord{
				{SignalType: "cost_spike", Source: "monitoring"},
				{SignalType: "cost_spike", Source: "monitoring"},
				{SignalType: "new_opportunity", Source: "external"},
			},
		},
		outcomes: &mockOutcomeProvider{
			outcomes: []OutcomeRecord{
				{ActionType: "summarize_state", Status: "succeeded", GoalType: "system_reliability"},
				{ActionType: "summarize_state", Status: "succeeded", GoalType: "system_reliability"},
				{ActionType: "summarize_state", Status: "succeeded", GoalType: "system_reliability"},
			},
		},
		proposals: &mockProposalProvider{},
	}

	result := engine.Run(context.Background())

	// Should match:
	// - repeated_manual_work (3x summarize_state)
	// - repeated_solved_issue (3x system_reliability succeeded)
	// - inbound_need (1x new_opportunity)
	// - cost_waste (2x cost_spike)
	if result.CandidatesCreated < 3 {
		t.Errorf("expected at least 3 candidates, got %d", result.CandidatesCreated)
	}

	// Check audit events were emitted
	foundRunEvent := false
	for _, e := range auditor.events {
		if e.eventType == "income.discovery_run" {
			foundRunEvent = true
		}
	}
	if !foundRunEvent {
		t.Error("expected income.discovery_run audit event")
	}
}

// Test 13: No input signals → no candidates
func TestEngine_NoInputsNoCandidates(t *testing.T) {
	auditor := &mockAuditRecorder{}
	logger := noopLogger()

	engine := &Engine{
		rules:     DefaultRules(),
		auditor:   auditor,
		logger:    logger,
		signals:   &mockSignalProvider{},
		outcomes:  &mockOutcomeProvider{},
		proposals: &mockProposalProvider{},
	}

	result := engine.Run(context.Background())

	if result.CandidatesCreated != 0 {
		t.Errorf("expected 0 candidates, got %d", result.CandidatesCreated)
	}
}

// Test 14: Nil provider / nil store fail-open
func TestEngine_NilProviderFailOpen(t *testing.T) {
	auditor := &mockAuditRecorder{}
	logger := noopLogger()

	engine := &Engine{
		rules:   DefaultRules(),
		auditor: auditor,
		logger:  logger,
		// All providers are nil
	}

	result := engine.Run(context.Background())

	// Should not panic and should produce no candidates
	if result.CandidatesCreated != 0 {
		t.Errorf("expected 0 candidates with nil providers, got %d", result.CandidatesCreated)
	}
}

// Test: Engine with nil store still runs
func TestEngine_NilStoreRunsOK(t *testing.T) {
	auditor := &mockAuditRecorder{}
	logger := noopLogger()

	engine := &Engine{
		rules:   DefaultRules(),
		auditor: auditor,
		logger:  logger,
		signals: &mockSignalProvider{
			signals: []SignalRecord{
				{SignalType: "cost_spike", Source: "monitoring"},
				{SignalType: "cost_spike", Source: "monitoring"},
			},
		},
		outcomes:  &mockOutcomeProvider{},
		proposals: &mockProposalProvider{},
		// store is nil
	}

	result := engine.Run(context.Background())

	if result.CandidatesCreated < 1 {
		t.Error("expected candidates even with nil store")
	}
}

// ---------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------

// confidenceFromCount tests
func TestConfidenceFromCount(t *testing.T) {
	// At threshold exactly
	c := confidenceFromCount(3, 3)
	if c != 0.3 {
		t.Errorf("expected 0.3 at threshold, got %f", c)
	}

	// Above threshold
	c = confidenceFromCount(10, 3)
	if c < 0.3 || c > 0.95 {
		t.Errorf("expected confidence in [0.3, 0.95], got %f", c)
	}

	// Way above threshold
	c = confidenceFromCount(100, 3)
	if c != 0.95 {
		t.Errorf("expected max confidence 0.95, got %f", c)
	}

	// Zero count
	c = confidenceFromCount(0, 3)
	if c != 0.3 {
		t.Errorf("expected 0.3 for zero count, got %f", c)
	}
}

func TestIsManualAction(t *testing.T) {
	tests := []struct {
		action string
		want   bool
	}{
		{"summarize_state", true},
		{"generate_report", true},
		{"manual_check", true},
		{"operator_fix", true},
		{"review_changes", true},
		{"triage_alerts", true},
		{"deploy_service", false},
		{"fix_bug", false},
	}
	for _, tt := range tests {
		got := isManualAction(tt.action)
		if got != tt.want {
			t.Errorf("isManualAction(%q) = %v, want %v", tt.action, got, tt.want)
		}
	}
}

func TestCandidateTypes_Valid(t *testing.T) {
	types := []string{
		CandidateAutomation,
		CandidateResaleRepackage,
		CandidateConsultingLead,
		CandidateCostSaving,
		CandidateProductFeature,
	}
	for _, ct := range types {
		if !ValidCandidateTypes[ct] {
			t.Errorf("candidate type %q not in ValidCandidateTypes", ct)
		}
	}
}

func TestEstimateValues_Capped(t *testing.T) {
	// Test that value estimation is capped at MaxOpValue
	v := estimateAutomationValue(1000)
	if v > MaxOpValue {
		t.Errorf("automation value %f exceeds MaxOpValue %f", v, MaxOpValue)
	}
	v = estimateConsultingValue(100)
	if v > MaxOpValue {
		t.Errorf("consulting value %f exceeds MaxOpValue %f", v, MaxOpValue)
	}
}

// ---------------------------------------------------------------
// Audit event tests
// ---------------------------------------------------------------

func TestEngine_EmitsAuditEvents(t *testing.T) {
	auditor := &mockAuditRecorder{}
	logger := noopLogger()

	engine := &Engine{
		rules:   DefaultRules(),
		auditor: auditor,
		logger:  logger,
		signals: &mockSignalProvider{
			signals: []SignalRecord{
				{SignalType: "cost_spike", Source: "monitoring"},
				{SignalType: "cost_spike", Source: "monitoring"},
			},
		},
		outcomes:  &mockOutcomeProvider{},
		proposals: &mockProposalProvider{},
	}

	engine.Run(context.Background())

	eventTypes := map[string]bool{}
	for _, e := range auditor.events {
		eventTypes[e.eventType] = true
	}

	if !eventTypes["income.discovery_run"] {
		t.Error("expected income.discovery_run audit event")
	}
	if !eventTypes["income.discovery_candidate_created"] {
		t.Error("expected income.discovery_candidate_created audit event")
	}
}

// ---------------------------------------------------------------
// Helpers for deduplication tests
// ---------------------------------------------------------------

// memCandidateChecker implements CandidateChecker for unit tests.
type memCandidateChecker struct {
	candidates    []DiscoveryCandidate
	respectWindow bool
}

func (m *memCandidateChecker) FindByDedupeKey(_ context.Context, key, ctype string, windowHours int) (*DiscoveryCandidate, error) {
	for i, c := range m.candidates {
		if c.DedupeKey == key && c.CandidateType == ctype && c.Status != CandidateStatusSkipped {
			if m.respectWindow {
				cutoff := time.Now().Add(-time.Duration(windowHours) * time.Hour)
				if c.CreatedAt.Before(cutoff) {
					return nil, nil
				}
			}
			return &m.candidates[i], nil
		}
	}
	return nil, nil
}

func (m *memCandidateChecker) IncrementEvidence(_ context.Context, id string) error {
	for i, c := range m.candidates {
		if c.ID == id {
			m.candidates[i].EvidenceCount++
			return nil
		}
	}
	return nil
}

// noopLogger returns a zap.Logger that discards all output.
func noopLogger() *zap.Logger {
	return zap.NewNop()
}
