package autonomy

import (
	"context"

	"github.com/tiroq/arcanum/internal/api"
)

// APIAdapter wraps an Orchestrator to satisfy the api.AutonomyOrchestrator interface.
type APIAdapter struct {
	orch       *Orchestrator
	configPath string
}

// NewAPIAdapter creates an adapter that bridges the Orchestrator to the API layer.
func NewAPIAdapter(orch *Orchestrator, configPath string) *APIAdapter {
	return &APIAdapter{orch: orch, configPath: configPath}
}

func (a *APIAdapter) Start(ctx context.Context) error {
	return a.orch.Start(ctx)
}

func (a *APIAdapter) Stop(ctx context.Context) {
	a.orch.Stop(ctx)
}

func (a *APIAdapter) GetState() api.AutonomyRuntimeState {
	s := a.orch.GetState()
	return api.AutonomyRuntimeState{
		Mode:                 string(s.Mode),
		OriginalMode:         string(s.OriginalMode),
		Running:              s.Running,
		StartedAt:            s.StartedAt,
		CyclesRun:            s.CyclesRun,
		ConsecutiveFailures:  s.ConsecutiveFailures,
		Downgraded:           s.Downgraded,
		DowngradeReason:      s.DowngradeReason,
		HeavyActionsDisabled: s.HeavyActionsDisabled,
		SelfExtDisabled:      s.SelfExtDisabled,
		SafeActionsRouted:    s.SafeActionsRouted,
		ReviewActionsQueued:  s.ReviewActionsQueued,
		SuppressedDecisions:  s.SuppressedDecisions,
		ReportCount:          s.ReportCount,
		LastError:            s.LastError,
	}
}

func (a *APIAdapter) GetReports(limit int) []api.AutonomyReportView {
	reports := a.orch.GetReports(limit)
	result := make([]api.AutonomyReportView, len(reports))
	for i, r := range reports {
		result[i] = api.AutonomyReportView{
			ID:                r.ID,
			Type:              r.Type,
			CreatedAt:         r.CreatedAt,
			Mode:              r.Mode,
			CyclesRun:         r.CyclesRun,
			SafeActionsRouted: r.SafeActionsRouted,
			ReviewQueued:      r.ReviewQueued,
			SuppressedCount:   r.SuppressedCount,
			Downgraded:        r.Downgraded,
			DowngradeReason:   r.DowngradeReason,
			FailureCount:      r.FailureCount,
			Warnings:          r.Warnings,
			ExceptionTrigger:  r.ExceptionTrigger,
		}
	}
	return result
}

func (a *APIAdapter) ReloadConfig(ctx context.Context, path string) error {
	if path == "" {
		path = a.configPath
	}
	cfg, err := LoadAutonomyConfig(path)
	if err != nil {
		return err
	}
	return a.orch.ReloadConfig(ctx, cfg)
}

func (a *APIAdapter) SetMode(ctx context.Context, mode string) error {
	return a.orch.SetMode(ctx, AutonomyMode(mode))
}
