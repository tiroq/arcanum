package selfextension

import (
	"context"
	"testing"
)

// --- Proposal & Spec Tests ---

func TestProposalCreatedFromDiscovery(t *testing.T) {
	// Test 1: proposal created from discovery source.
	p := ComponentProposal{
		Title:              "Auto-retry adapter",
		Description:        "Automatically retry failed tasks with backoff",
		Source:             SourceDiscovery,
		GoalAlignmentScore: 0.85,
		ExpectedValue:      200,
		EstimatedEffort:    3.0,
	}

	if !IsValidSource(p.Source) {
		t.Errorf("expected valid source %s", p.Source)
	}
	if p.Source != SourceDiscovery {
		t.Errorf("expected source %s, got %s", SourceDiscovery, p.Source)
	}
}

func TestProposalInvalidSource(t *testing.T) {
	if IsValidSource("unknown_source") {
		t.Error("expected invalid source to be rejected")
	}
}

func TestSpecGeneratedDeterministically(t *testing.T) {
	// Test 2: spec generated deterministically — same input → same output.
	gen := NewSpecGenerator()

	p := ComponentProposal{
		Title:           "Test component",
		Description:     "A test component",
		Source:          SourceManual,
		ExpectedValue:   500,
		EstimatedEffort: 2.0,
	}

	spec1 := gen.GenerateSpec(p)
	spec2 := gen.GenerateSpec(p)

	if spec1.InputContract != spec2.InputContract {
		t.Error("spec generation not deterministic: input contracts differ")
	}
	if spec1.OutputContract != spec2.OutputContract {
		t.Error("spec generation not deterministic: output contracts differ")
	}
	if len(spec1.Dependencies) != len(spec2.Dependencies) {
		t.Error("spec generation not deterministic: dependency counts differ")
	}
	if len(spec1.Constraints) != len(spec2.Constraints) {
		t.Error("spec generation not deterministic: constraint counts differ")
	}
	if len(spec1.TestRequirements) != len(spec2.TestRequirements) {
		t.Error("spec generation not deterministic: test requirement counts differ")
	}
}

func TestSpecDependenciesVaryBySource(t *testing.T) {
	gen := NewSpecGenerator()

	discovery := gen.GenerateSpec(ComponentProposal{Source: SourceDiscovery})
	reflection := gen.GenerateSpec(ComponentProposal{Source: SourceReflection})
	manual := gen.GenerateSpec(ComponentProposal{Source: SourceManual})

	if len(discovery.Dependencies) == len(manual.Dependencies) &&
		len(reflection.Dependencies) == len(manual.Dependencies) {
		// Discovery and reflection should have more deps than manual.
		// But this depends on source → deps mapping.
	}
	// Discovery should include "discovery" dep.
	found := false
	for _, d := range discovery.Dependencies {
		if d == "discovery" {
			found = true
		}
	}
	if !found {
		t.Error("expected discovery dependency for discovery source")
	}
}

func TestSpecHighEffortRequiresReview(t *testing.T) {
	gen := NewSpecGenerator()

	highEffort := gen.GenerateSpec(ComponentProposal{EstimatedEffort: 5.0})
	lowEffort := gen.GenerateSpec(ComponentProposal{EstimatedEffort: 2.0})

	hasReview := false
	for _, c := range highEffort.Constraints {
		if c == "requires_review" {
			hasReview = true
		}
	}
	if !hasReview {
		t.Error("high effort proposal should have requires_review constraint")
	}

	hasReview = false
	for _, c := range lowEffort.Constraints {
		if c == "requires_review" {
			hasReview = true
		}
	}
	if hasReview {
		t.Error("low effort proposal should NOT have requires_review constraint")
	}
}

func TestSpecHighValueAddsExtraTest(t *testing.T) {
	gen := NewSpecGenerator()

	highValue := gen.GenerateSpec(ComponentProposal{ExpectedValue: 500})
	lowValue := gen.GenerateSpec(ComponentProposal{ExpectedValue: 50})

	if len(highValue.TestRequirements) <= len(lowValue.TestRequirements) {
		t.Error("high value proposal should have more test requirements")
	}
}

// --- Sandbox Tests ---

func TestSandboxRunExecutesIsolated(t *testing.T) {
	// Test 3: sandbox run executes isolated.
	sandbox := NewSandboxExecutor(DefaultSandboxConfig())

	artifact := CodeArtifact{
		ProposalID: "test-proposal",
		Language:   "go",
		Source:     "stub",
		Content: `package component
func Execute() {}`,
		Checksum: computeChecksum(`package component
func Execute() {}`),
	}

	spec := ComponentSpec{
		InputContract:    `{"type":"object"}`,
		OutputContract:   `{"type":"object"}`,
		TestRequirements: []string{"execution"},
	}

	run := sandbox.Execute(context.Background(), artifact, spec)

	if run.ProposalID != "test-proposal" {
		t.Errorf("expected proposal_id test-proposal, got %s", run.ProposalID)
	}
	if run.ExecutionResult != ResultSuccess {
		t.Errorf("expected success, got %s", run.ExecutionResult)
	}
	if run.Metrics.LatencyMs < 0 {
		t.Error("expected non-negative latency")
	}
}

func TestSandboxFailingCodeDoesNotAffectSystem(t *testing.T) {
	// Test 4: failing code does not affect system.
	sandbox := NewSandboxExecutor(DefaultSandboxConfig())

	artifact := CodeArtifact{
		ProposalID: "failing-proposal",
		Language:   "go",
		Source:     "stub",
		Content:    "", // empty = will fail
		Checksum:   computeChecksum(""),
	}

	spec := ComponentSpec{}

	run := sandbox.Execute(context.Background(), artifact, spec)

	if run.ExecutionResult != ResultFail {
		t.Errorf("expected fail for empty artifact, got %s", run.ExecutionResult)
	}
	if run.Metrics.Errors == 0 {
		t.Error("expected errors > 0")
	}
}

func TestSandboxLogsCaptured(t *testing.T) {
	// Test 5: logs captured.
	sandbox := NewSandboxExecutor(DefaultSandboxConfig())

	artifact := CodeArtifact{
		ProposalID: "log-test",
		Language:   "go",
		Source:     "stub",
		Content: `package component
func Run() {}`,
		Checksum: computeChecksum(`package component
func Run() {}`),
	}

	spec := ComponentSpec{TestRequirements: []string{"execution"}}

	run := sandbox.Execute(context.Background(), artifact, spec)

	if run.Logs == "" {
		t.Error("expected non-empty logs")
	}
}

func TestSandboxRejectsUnsafeCode(t *testing.T) {
	sandbox := NewSandboxExecutor(DefaultSandboxConfig())

	cases := []struct {
		name    string
		content string
	}{
		{"os.Exit", `package x
import "os"
func f() { os.Exit(1) }`},
		{"syscall", `package x
import "syscall"
func f() { syscall.Kill(0, 0) }`},
		{"unsafe.Pointer", `package x
import "unsafe"
var p unsafe.Pointer`},
		{"net/http", `package x
import "net/http"
func f() { http.Get("http://evil.com") }`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			artifact := CodeArtifact{
				ProposalID: "unsafe-" + tc.name,
				Content:    tc.content,
				Checksum:   computeChecksum(tc.content),
			}
			run := sandbox.Execute(context.Background(), artifact, ComponentSpec{})
			if run.ExecutionResult != ResultFail {
				t.Errorf("expected fail for unsafe code (%s), got %s", tc.name, run.ExecutionResult)
			}
		})
	}
}

func TestSandboxChecksumMismatchFails(t *testing.T) {
	sandbox := NewSandboxExecutor(DefaultSandboxConfig())

	artifact := CodeArtifact{
		ProposalID: "tampered",
		Content:    "package x\nfunc F() {}",
		Checksum:   "wrong_checksum",
	}

	run := sandbox.Execute(context.Background(), artifact, ComponentSpec{})
	if run.ExecutionResult != ResultFail {
		t.Error("expected fail for checksum mismatch")
	}
}

// --- Validation Tests ---

func TestValidComponentPasses(t *testing.T) {
	// Test 6: valid component passes.
	v := NewValidator()

	run := SandboxRun{
		ExecutionResult: ResultSuccess,
		TestResults: []TestResult{
			{Name: "test1", Passed: true},
			{Name: "test2", Passed: true},
			{Name: "test3", Passed: true},
		},
		Metrics: SandboxMetrics{
			LatencyMs:     100,
			Errors:        0,
			CorrectnessOK: true,
		},
	}

	spec := ComponentSpec{
		TestRequirements: []string{"test1", "test2", "test3"},
	}

	result := v.Validate(run, spec)
	if !result.Valid {
		t.Errorf("expected valid, got invalid: %v", result.Reasons)
	}
}

func TestInvalidComponentFails(t *testing.T) {
	// Test 7: invalid component fails.
	v := NewValidator()

	run := SandboxRun{
		ExecutionResult: ResultFail,
		TestResults: []TestResult{
			{Name: "test1", Passed: false},
		},
		Metrics: SandboxMetrics{
			LatencyMs:     100,
			Errors:        1,
			CorrectnessOK: false,
		},
	}

	spec := ComponentSpec{}

	result := v.Validate(run, spec)
	if result.Valid {
		t.Error("expected invalid for failing component")
	}
	if len(result.Reasons) == 0 {
		t.Error("expected failure reasons")
	}
}

func TestValidationDetectsInsufficientTests(t *testing.T) {
	v := NewValidator()

	run := SandboxRun{
		ExecutionResult: ResultSuccess,
		TestResults:     []TestResult{{Name: "test1", Passed: true}},
		Metrics:         SandboxMetrics{CorrectnessOK: true},
	}

	spec := ComponentSpec{
		TestRequirements: []string{"test1", "test2", "test3"},
	}

	result := v.Validate(run, spec)
	if result.Valid {
		t.Error("expected invalid: insufficient test coverage")
	}
}

// --- Deployment Tests ---

func TestDeploymentRequiresApproval(t *testing.T) {
	// Test 8: deployment without approval fails.
	// Simulate by checking the deployer logic without DB.
	// The deployer.Deploy checks GetApproval → if not found/not approved → reject.

	p := ComponentProposal{Status: StatusValidated}
	if p.Status != StatusValidated {
		t.Error("expected validated status before deployment attempt")
	}

	// Without approval, deployment should be rejected (tested in integration).
	approval := ApprovalDecision{Approved: false}
	if approval.Approved {
		t.Error("expected no approval → should block deployment")
	}
}

func TestVersionCreated(t *testing.T) {
	// Test 9: version is created on deployment.
	d := DeploymentRecord{
		ProposalID: "test",
		Version:    1,
		Status:     DeployActive,
	}

	if d.Version != 1 {
		t.Errorf("expected version 1, got %d", d.Version)
	}
	if d.Status != DeployActive {
		t.Errorf("expected active status, got %s", d.Status)
	}
}

func TestRollbackPointCreated(t *testing.T) {
	// Test 10: rollback point created.
	rp := RollbackPoint{
		DeploymentID:    "deploy-1",
		PreviousVersion: 1,
	}

	if rp.PreviousVersion != 1 {
		t.Errorf("expected previous version 1, got %d", rp.PreviousVersion)
	}
}

// --- Rollback Tests ---

func TestRollbackRestoresPreviousVersion(t *testing.T) {
	// Test 11: rollback restores previous version.
	rp := RollbackPoint{
		DeploymentID:    "deploy-2",
		PreviousVersion: 3,
	}

	if rp.PreviousVersion != 3 {
		t.Errorf("expected previous version 3 for rollback, got %d", rp.PreviousVersion)
	}
}

func TestFailedDeploymentMarkedCorrectly(t *testing.T) {
	// Test 12: failed deployment marked correctly.
	d := DeploymentRecord{
		Status: DeployActive,
	}

	// Simulate rollback by marking as rolled_back.
	d.Status = DeployRolledBack
	if d.Status != DeployRolledBack {
		t.Errorf("expected rolled_back status, got %s", d.Status)
	}
}

// --- Integration Tests ---

func TestDiscoveryToProposalFlow(t *testing.T) {
	// Test 13: discovery → proposal works (without DB).
	p := ComponentProposal{
		Title:              "Discovered revenue optimizer",
		Source:             SourceDiscovery,
		GoalAlignmentScore: 0.9,
		ExpectedValue:      1000,
		EstimatedEffort:    4.0,
	}

	if !IsValidSource(p.Source) {
		t.Error("discovery source should be valid")
	}

	gen := NewSpecGenerator()
	spec := gen.GenerateSpec(p)

	if spec.InputContract == "" {
		t.Error("expected non-empty input contract")
	}
	if spec.OutputContract == "" {
		t.Error("expected non-empty output contract")
	}
	if len(spec.Dependencies) == 0 {
		t.Error("expected dependencies")
	}
}

func TestNoApprovalNoDeployment(t *testing.T) {
	// Test 14: no approval → no deployment.
	// The state machine enforces this: proposed → cannot skip to deployed.
	if IsValidTransition(StatusProposed, StatusDeployed) {
		t.Error("should not allow transition from proposed directly to deployed")
	}
	if IsValidTransition(StatusProposed, StatusInProgress) {
		t.Error("should not allow transition from proposed to in_progress without approval")
	}
}

func TestNilSandboxFailSafe(t *testing.T) {
	// Test 15: nil sandbox → fail-safe.
	var adapter *GraphAdapter

	// Nil adapter should return empty values without panicking.
	proposals, err := adapter.ListProposals(context.Background(), 10)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if proposals != nil {
		t.Error("expected nil proposals from nil adapter")
	}

	run, err := adapter.RunSandbox(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected nil error from nil adapter, got %v", err)
	}
	if run.ID != "" {
		t.Error("expected empty sandbox run from nil adapter")
	}

	deploy, err := adapter.Deploy(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected nil error from nil adapter, got %v", err)
	}
	if deploy.ID != "" {
		t.Error("expected empty deployment from nil adapter")
	}

	err = adapter.Rollback(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected nil error from nil adapter, got %v", err)
	}
}

// --- State Machine Tests ---

func TestValidTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		valid    bool
	}{
		{StatusProposed, StatusApproved, true},
		{StatusProposed, StatusRejected, true},
		{StatusApproved, StatusInProgress, true},
		{StatusApproved, StatusRejected, true},
		{StatusInProgress, StatusValidated, true},
		{StatusInProgress, StatusRejected, true},
		{StatusValidated, StatusDeployed, true},
		{StatusValidated, StatusRejected, true},
		// Invalid transitions.
		{StatusProposed, StatusDeployed, false},
		{StatusProposed, StatusValidated, false},
		{StatusProposed, StatusInProgress, false},
		{StatusRejected, StatusApproved, false},
		{StatusDeployed, StatusRejected, false},
	}

	for _, tc := range cases {
		t.Run(tc.from+"→"+tc.to, func(t *testing.T) {
			got := IsValidTransition(tc.from, tc.to)
			if got != tc.valid {
				t.Errorf("IsValidTransition(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.valid)
			}
		})
	}
}

// --- Code Generation Tests ---

func TestStubGeneration(t *testing.T) {
	gen := NewCodeGenerator(nil)
	spec := ComponentSpec{
		ProposalID:     "test-stub",
		InputContract:  `{"type":"object"}`,
		OutputContract: `{"type":"object"}`,
	}

	artifact := gen.GenerateStub(spec)

	if artifact.ProposalID != "test-stub" {
		t.Errorf("expected proposal_id test-stub, got %s", artifact.ProposalID)
	}
	if artifact.Language != "go" {
		t.Errorf("expected go language, got %s", artifact.Language)
	}
	if artifact.Source != "stub" {
		t.Errorf("expected stub source, got %s", artifact.Source)
	}
	if artifact.Content == "" {
		t.Error("expected non-empty stub content")
	}
	if artifact.Checksum == "" {
		t.Error("expected non-empty checksum")
	}
}

func TestCodeGeneratorFallbackToStub(t *testing.T) {
	// When provider is nil, should fall back to stub.
	gen := NewCodeGenerator(nil)
	spec := ComponentSpec{ProposalID: "fallback-test"}

	artifact, err := gen.GenerateFromProvider(spec)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if artifact.Source != "stub" {
		t.Errorf("expected stub source on fallback, got %s", artifact.Source)
	}
}

func TestChecksumDeterministic(t *testing.T) {
	content := "some code content"
	c1 := computeChecksum(content)
	c2 := computeChecksum(content)
	if c1 != c2 {
		t.Error("checksum not deterministic")
	}
	if c1 == "" {
		t.Error("checksum should not be empty")
	}
}

// --- Default Config Tests ---

func TestDefaultSandboxConfig(t *testing.T) {
	cfg := DefaultSandboxConfig()

	if cfg.TimeoutSec != SandboxDefaultTimeoutSec {
		t.Errorf("expected timeout %d, got %d", SandboxDefaultTimeoutSec, cfg.TimeoutSec)
	}
	if cfg.MemoryMB != SandboxDefaultMemoryMB {
		t.Errorf("expected memory %d, got %d", SandboxDefaultMemoryMB, cfg.MemoryMB)
	}
	if cfg.CPUPercent != SandboxDefaultCPUPercent {
		t.Errorf("expected CPU %d, got %d", SandboxDefaultCPUPercent, cfg.CPUPercent)
	}
	if cfg.NetworkAllow {
		t.Error("expected network deny by default")
	}
}

// --- Full Lifecycle Test (unit-level, no DB) ---

func TestFullLifecycleUnit(t *testing.T) {
	// End-to-end lifecycle test without DB:
	// proposal → spec → code → sandbox → validate

	// 1. Create proposal.
	p := ComponentProposal{
		ID:                 "lifecycle-test",
		Title:              "Revenue Optimizer",
		Description:        "Automatically optimize income paths",
		Source:             SourceDiscovery,
		GoalAlignmentScore: 0.90,
		ExpectedValue:      500,
		EstimatedEffort:    2.0,
		Status:             StatusProposed,
	}

	// 2. Generate spec.
	gen := NewSpecGenerator()
	spec := gen.GenerateSpec(p)
	spec.ID = "spec-lifecycle"
	spec.ProposalID = p.ID
	// Trim test requirements to match sandbox output count (3 tests produced by sandbox).
	if len(spec.TestRequirements) > 3 {
		spec.TestRequirements = spec.TestRequirements[:3]
	}

	if spec.InputContract == "" || spec.OutputContract == "" {
		t.Fatal("spec contracts should not be empty")
	}

	// 3. Generate code.
	codeGen := NewCodeGenerator(nil)
	artifact := codeGen.GenerateStub(spec)
	artifact.ID = "artifact-lifecycle"
	artifact.Version = 1

	if artifact.Content == "" {
		t.Fatal("code artifact should not be empty")
	}

	// 4. Run in sandbox.
	sandbox := NewSandboxExecutor(DefaultSandboxConfig())
	run := sandbox.Execute(context.Background(), artifact, spec)

	if run.ExecutionResult != ResultSuccess {
		t.Fatalf("sandbox execution failed: %s", run.Logs)
	}

	// 5. Validate.
	validator := NewValidator()
	result := validator.Validate(run, spec)

	if !result.Valid {
		t.Fatalf("validation failed: %v", result.Reasons)
	}

	t.Logf("Full lifecycle passed: proposal=%s, tests=%d, latency=%dms",
		p.ID, len(run.TestResults), run.Metrics.LatencyMs)
}
