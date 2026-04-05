package processors

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/providers"
)

// JobContext is passed to all processors.
type JobContext struct {
	JobID           uuid.UUID
	SourceTaskID    uuid.UUID
	JobType         string
	SnapshotPayload json.RawMessage
	PriorResults    []models.ProcessingRun
}

// ProcessResult is the structured output of a processor.
type ProcessResult struct {
	ProposalType          string
	OutputPayload         json.RawMessage
	HumanReviewRequired   bool
	Outcome               string // "success", "failure", "error"
	ErrorMessage          string
	PromptTemplateID      string
	PromptTemplateVersion string
	ModelProvider         string
	ModelRole             providers.ModelRole
	ModelName             string
	TokensPrompt          int
	TokensCompletion      int
	TokensUsed            int // total (prompt + completion)
	DurationMS            int64
	TimeoutUsed           time.Duration
	ExecutionTrace        json.RawMessage
	// UsedFallback is true when the provider resolved to the default model
	// because no role-specific model was configured.
	UsedFallback bool
}

// Processor is the abstraction for all processing implementations.
type Processor interface {
	Name() string
	Version() string
	CanHandle(jobType string) bool
	Process(ctx context.Context, jc JobContext) (ProcessResult, error)
}

// Registry holds all registered processors.
type Registry struct {
	mu         sync.RWMutex
	processors []Processor
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a processor to the registry.
func (r *Registry) Register(p Processor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processors = append(r.processors, p)
}

// FindFor returns the first processor that can handle the given jobType.
func (r *Registry) FindFor(jobType string) (Processor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.processors {
		if p.CanHandle(jobType) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no processor found for job type %q", jobType)
}

// All returns all registered processors.
func (r *Registry) All() []Processor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Processor, len(r.processors))
	copy(out, r.processors)
	return out
}
