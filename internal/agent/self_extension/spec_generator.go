package selfextension

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// SpecGenerator converts a ComponentProposal into a deterministic ComponentSpec.
// This is a pure function — no side effects, no network, no randomness.
type SpecGenerator struct{}

// NewSpecGenerator creates a new SpecGenerator.
func NewSpecGenerator() *SpecGenerator {
	return &SpecGenerator{}
}

// GenerateSpec converts a proposal into a formal component spec.
// The spec is deterministic: same proposal always produces the same spec structure.
func (g *SpecGenerator) GenerateSpec(proposal ComponentProposal) ComponentSpec {
	inputContract := g.buildInputContract(proposal)
	outputContract := g.buildOutputContract(proposal)

	return ComponentSpec{
		InputContract:    inputContract,
		OutputContract:   outputContract,
		Dependencies:     g.deriveDependencies(proposal),
		Constraints:      g.deriveConstraints(proposal),
		TestRequirements: g.deriveTestRequirements(proposal),
	}
}

func (g *SpecGenerator) buildInputContract(p ComponentProposal) string {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"proposal_id": map[string]string{"type": "string"},
			"context":     map[string]string{"type": "object"},
		},
		"required": []string{"proposal_id"},
	}
	b, _ := json.Marshal(schema)
	return string(b)
}

func (g *SpecGenerator) buildOutputContract(p ComponentProposal) string {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]string{"type": "boolean"},
			"result":  map[string]string{"type": "object"},
			"error":   map[string]string{"type": "string"},
		},
		"required": []string{"success"},
	}
	b, _ := json.Marshal(schema)
	return string(b)
}

func (g *SpecGenerator) deriveDependencies(p ComponentProposal) []string {
	deps := []string{"audit", "store"}
	switch p.Source {
	case SourceDiscovery:
		deps = append(deps, "discovery")
	case SourceReflection:
		deps = append(deps, "reflection")
	case SourceFailure:
		deps = append(deps, "outcome")
	}
	return deps
}

func (g *SpecGenerator) deriveConstraints(p ComponentProposal) []string {
	constraints := []string{
		"no_network_access",
		"no_filesystem_write_outside_sandbox",
		"max_execution_time_60s",
		"no_secret_access",
	}
	if p.EstimatedEffort > 4.0 {
		constraints = append(constraints, "requires_review")
	}
	return constraints
}

func (g *SpecGenerator) deriveTestRequirements(p ComponentProposal) []string {
	reqs := []string{
		"input_validation_test",
		"output_schema_test",
		"error_handling_test",
	}
	if p.ExpectedValue > 100 {
		reqs = append(reqs, "value_verification_test")
	}
	return reqs
}

// CodeGenerator generates stub/template code for a component.
// LLM-assisted generation is behind the CodeGenerationProvider interface.
type CodeGenerator struct {
	provider CodeGenerationProvider
}

// CodeGenerationProvider is the interface for optional LLM-assisted code generation.
// Implementations must be deterministic in structure even if content varies.
type CodeGenerationProvider interface {
	GenerateCode(spec ComponentSpec) (string, error)
}

// NewCodeGenerator creates a new CodeGenerator with an optional LLM provider.
func NewCodeGenerator(provider CodeGenerationProvider) *CodeGenerator {
	return &CodeGenerator{provider: provider}
}

// GenerateStub generates a minimal stub implementation from a spec.
// This is always available, deterministic, and safe.
func (g *CodeGenerator) GenerateStub(spec ComponentSpec) CodeArtifact {
	stub := fmt.Sprintf(`package component

// Auto-generated stub for proposal: %s
// Input contract: %s
// Output contract: %s

import "context"

type Component struct{}

func New() *Component { return &Component{} }

func (c *Component) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	return map[string]any{"success": true, "result": map[string]any{}}, nil
}
`, spec.ProposalID, spec.InputContract, spec.OutputContract)

	return CodeArtifact{
		ProposalID: spec.ProposalID,
		Language:   "go",
		Source:     "stub",
		Content:    stub,
		Checksum:   computeChecksum(stub),
	}
}

// GenerateFromProvider attempts LLM-assisted code generation.
// Falls back to stub if provider is nil or fails.
func (g *CodeGenerator) GenerateFromProvider(spec ComponentSpec) (CodeArtifact, error) {
	if g.provider == nil {
		stub := g.GenerateStub(spec)
		stub.Source = "stub"
		return stub, nil
	}

	code, err := g.provider.GenerateCode(spec)
	if err != nil {
		// Fail-safe: fall back to stub on LLM failure.
		stub := g.GenerateStub(spec)
		stub.Source = "stub"
		return stub, nil
	}

	return CodeArtifact{
		ProposalID: spec.ProposalID,
		Language:   "go",
		Source:     "llm_assisted",
		Content:    code,
		Checksum:   computeChecksum(code),
	}, nil
}

func computeChecksum(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}
