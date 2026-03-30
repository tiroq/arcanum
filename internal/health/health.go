package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	nats "github.com/nats-io/nats.go"
)

// ProviderHealthChecker is implemented by LLM providers that support health checks.
type ProviderHealthChecker interface {
	Name() string
	HealthCheck(ctx context.Context) error
}

type statusResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// HealthHandler always returns 200 OK with {"status":"ok"}.
// It is used for liveness probes.
func HealthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(statusResponse{Status: "ok"}) //nolint:errcheck
}

// ReadinessChecker holds optional dependencies to check for readiness probes.
type ReadinessChecker struct {
	DB        *pgxpool.Pool
	NATS      *nats.Conn
	Providers []ProviderHealthChecker
}

// ReadinessHandler checks DB, NATS, and provider connectivity before returning 200.
// Returns 503 if any dependency is unhealthy.
func (rc *ReadinessChecker) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := make(map[string]string)
	healthy := true

	if rc.DB != nil {
		if err := rc.DB.Ping(ctx); err != nil {
			checks["database"] = "unhealthy: " + err.Error()
			healthy = false
		} else {
			checks["database"] = "ok"
		}
	}

	if rc.NATS != nil {
		if !rc.NATS.IsConnected() {
			checks["nats"] = "unhealthy: not connected"
			healthy = false
		} else {
			checks["nats"] = "ok"
		}
	}

	for _, p := range rc.Providers {
		if err := p.HealthCheck(ctx); err != nil {
			checks["provider:"+p.Name()] = "unhealthy: " + err.Error()
			// Provider failure is degraded, not fatal.
		} else {
			checks["provider:"+p.Name()] = "ok"
		}
	}

	resp := statusResponse{Checks: checks}
	w.Header().Set("Content-Type", "application/json")
	if healthy {
		resp.Status = "ok"
		w.WriteHeader(http.StatusOK)
	} else {
		resp.Status = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
