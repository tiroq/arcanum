package processors

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// RulesOnlyProcessor applies deterministic transformations without any LLM call.
type RulesOnlyProcessor struct{}

// NewRulesOnlyProcessor creates a new RulesOnlyProcessor.
func NewRulesOnlyProcessor() *RulesOnlyProcessor {
	return &RulesOnlyProcessor{}
}

func (p *RulesOnlyProcessor) Name() string    { return "rules_only" }
func (p *RulesOnlyProcessor) Version() string { return "v1" }

func (p *RulesOnlyProcessor) CanHandle(jobType string) bool {
	return jobType == "rules_classify"
}

type rulesInput struct {
	Title string `json:"title"`
}

type rulesOutput struct {
	Action   string `json:"action"`
	Original string `json:"original"`
	Result   string `json:"result"`
}

func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if unicode.IsLower(r) {
				return false
			}
		}
	}
	return hasLetter
}

// Process applies rules-based normalization: trim trailing whitespace, convert ALL CAPS to Title Case.
func (p *RulesOnlyProcessor) Process(_ context.Context, jc JobContext) (ProcessResult, error) {
	var input rulesInput
	if len(jc.SnapshotPayload) > 0 {
		if err := json.Unmarshal(jc.SnapshotPayload, &input); err != nil {
			return ProcessResult{Outcome: "error", ErrorMessage: "unmarshal input: " + err.Error()}, nil
		}
	}

	original := strings.TrimRight(input.Title, " \t")
	result := original
	if isAllCaps(original) {
		result = cases.Title(language.English).String(strings.ToLower(original))
	}

	out, _ := json.Marshal(rulesOutput{
		Action:   "normalize",
		Original: original,
		Result:   result,
	})

	return ProcessResult{
		ProposalType:        "title_normalize",
		OutputPayload:       out,
		HumanReviewRequired: false,
		Outcome:             "success",
	}, nil
}
