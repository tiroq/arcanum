package providers

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// AuditedProvider wraps any Provider and records llm.started / llm.finished
// audit events for every Generate call. The job UUID is read from context via
// audit.JobIDKey — set by runner.go before calling proc.Process.
//
// If the context does not carry a job UUID, LLM events are not recorded (safe
// to use in testing contexts that don't inject a job ID).
type AuditedProvider struct {
	inner   Provider
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewAuditedProvider wraps inner with audit recording.
func NewAuditedProvider(inner Provider, auditor audit.AuditRecorder, logger *zap.Logger) *AuditedProvider {
	return &AuditedProvider{inner: inner, auditor: auditor, logger: logger}
}

func (p *AuditedProvider) Name() string { return p.inner.Name() }

func (p *AuditedProvider) HealthCheck(ctx context.Context) error {
	return p.inner.HealthCheck(ctx)
}

// Generate records llm.started before the call and llm.finished after,
// regardless of whether the call succeeds. Audit errors are logged and
// discarded — they never affect the provider response.
func (p *AuditedProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	jobID, hasJob := ctx.Value(audit.JobIDKey).(uuid.UUID)
	if !hasJob || jobID == uuid.Nil {
		// No job context — skip audit silently (e.g. test / health-check paths).
		return p.inner.Generate(ctx, req)
	}

	role := string(req.ModelRole)
	if role == "" {
		role = "default"
	}

	p.record(ctx, jobID, "llm.started", map[string]any{
		"provider": p.inner.Name(),
		"role":     role,
	})

	resp, err := p.inner.Generate(ctx, req)

	outcome := "success"
	payload := map[string]any{
		"provider":          p.inner.Name(),
		"model":             resp.Model,
		"role":              role,
		"duration_ms":       resp.DurationMS,
		"tokens_prompt":     resp.TokensPrompt,
		"tokens_completion": resp.TokensCompletion,
		"tokens_total":      resp.TokensTotal,
		"outcome":           outcome,
	}
	if err != nil {
		payload["outcome"] = "failure"
		payload["error"] = err.Error()
		if resp.ErrorClass != "" {
			payload["error_class"] = resp.ErrorClass
		}
	}
	p.record(context.WithoutCancel(ctx), jobID, "llm.finished", payload)

	return resp, err
}

// record is a fire-and-forget helper; errors are logged, never propagated.
func (p *AuditedProvider) record(ctx context.Context, jobID uuid.UUID, eventType string, payload any) {
	if err := p.auditor.RecordEvent(ctx, "job", jobID, eventType, "provider", p.inner.Name(), payload); err != nil {
		p.logger.Warn("audit record failed",
			zap.String("event_type", eventType),
			zap.String("job_id", jobID.String()),
			zap.Error(err),
		)
	}
}
