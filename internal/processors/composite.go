package processors

import (
	"context"
	"fmt"
)

// CompositeProcessor runs a sequence of sub-processors and collects all results.
type CompositeProcessor struct {
	name        string
	subProcessors []Processor
}

// NewCompositeProcessor creates a CompositeProcessor with the given sub-processors.
func NewCompositeProcessor(name string, subs ...Processor) *CompositeProcessor {
	return &CompositeProcessor{name: name, subProcessors: subs}
}

func (p *CompositeProcessor) Name() string    { return p.name }
func (p *CompositeProcessor) Version() string { return "v1" }

func (p *CompositeProcessor) CanHandle(jobType string) bool {
	return jobType == "composite"
}

// Process runs all sub-processors in sequence, stopping on first failure.
func (p *CompositeProcessor) Process(ctx context.Context, jc JobContext) (ProcessResult, error) {
	var lastResult ProcessResult

	for _, sub := range p.subProcessors {
		result, err := sub.Process(ctx, jc)
		if err != nil {
			return ProcessResult{
				Outcome:      "error",
				ErrorMessage: fmt.Sprintf("sub-processor %q error: %v", sub.Name(), err),
			}, err
		}
		if result.Outcome != "success" {
			return ProcessResult{
				Outcome:      "failure",
				ErrorMessage: fmt.Sprintf("sub-processor %q failed: %s", sub.Name(), result.ErrorMessage),
			}, nil
		}
		lastResult = result
	}

	return lastResult, nil
}
