// Package routing defines the explicit routing policy for model and provider selection.
//
// Routing policies answer: for a given execution role and set of available providers,
// which candidate chain should be used?
//
// Policy decisions are deterministic and explicitly configured at startup. Every
// decision is traceable from structured logs — the operator can always answer
// why a particular provider or model was chosen for a task.
package routing

import (
"fmt"
"strings"
)

// EscalationLevel defines how far a role is permitted to escalate through provider
// tiers when the primary candidate fails. Tiers are ordered by cost and capability:
// local (cheapest/fastest) → cloud → OpenRouter (most capable/costly).
type EscalationLevel string

const (
// EscalationLocalOnly restricts the role to the local Ollama provider only.
// No external provider escalation is permitted.
// Suitable for fast, latency-sensitive roles where external calls are undesirable.
EscalationLocalOnly EscalationLevel = "local_only"

// EscalationLocalCloud permits escalation from local Ollama to Ollama Cloud on failure.
// Suitable for roles that benefit from cloud capacity when local is unavailable.
EscalationLocalCloud EscalationLevel = "local_cloud"

// EscalationLocalCloudOpenRouter permits full escalation: local → Ollama Cloud → OpenRouter.
// Suitable for roles requiring the highest available capability as a last fallback.
EscalationLocalCloudOpenRouter EscalationLevel = "local_cloud_openrouter"

// EscalationLocalOpenRouter permits escalation from local directly to OpenRouter,
// bypassing the cloud tier (e.g. when cloud is unavailable or not applicable).
EscalationLocalOpenRouter EscalationLevel = "local_openrouter"
)

// allowsCloud reports whether this escalation level includes the Ollama Cloud tier.
func (e EscalationLevel) allowsCloud() bool {
return e == EscalationLocalCloud || e == EscalationLocalCloudOpenRouter
}

// allowsOpenRouter reports whether this escalation level includes the OpenRouter tier.
func (e EscalationLevel) allowsOpenRouter() bool {
return e == EscalationLocalCloudOpenRouter || e == EscalationLocalOpenRouter
}

// ParseEscalationLevel converts a string to an EscalationLevel.
// Accepts the canonical names and the aliases "local" and "full".
// Returns an error for unrecognized values.
func ParseEscalationLevel(s string) (EscalationLevel, error) {
switch strings.ToLower(strings.TrimSpace(s)) {
case "local_only", "local":
return EscalationLocalOnly, nil
case "local_cloud":
return EscalationLocalCloud, nil
case "local_cloud_openrouter", "full":
return EscalationLocalCloudOpenRouter, nil
case "local_openrouter":
return EscalationLocalOpenRouter, nil
default:
return EscalationLocalOnly, fmt.Errorf(
"unknown escalation level %q: must be one of local_only, local_cloud, local_cloud_openrouter, local_openrouter",
s,
)
}
}

// RolePolicy defines the routing policy for a single model execution role.
type RolePolicy struct {
// Escalation controls how far this role may escalate through provider tiers.
Escalation EscalationLevel
}

// RoutingPolicy is the top-level routing configuration for all model roles.
// It is constructed at startup from configuration and used to build execution profiles.
type RoutingPolicy struct {
Fast    RolePolicy
Default RolePolicy
Planner RolePolicy
Review  RolePolicy
}

// NewRoutingPolicy constructs a RoutingPolicy from raw string escalation level values
// as read from environment variables. Returns an error if any string is invalid.
func NewRoutingPolicy(fast, defaultLevel, planner, review string) (RoutingPolicy, error) {
fastLevel, err := ParseEscalationLevel(fast)
if err != nil {
return RoutingPolicy{}, fmt.Errorf("fast escalation: %w", err)
}
defaultLvl, err := ParseEscalationLevel(defaultLevel)
if err != nil {
return RoutingPolicy{}, fmt.Errorf("default escalation: %w", err)
}
plannerLevel, err := ParseEscalationLevel(planner)
if err != nil {
return RoutingPolicy{}, fmt.Errorf("planner escalation: %w", err)
}
reviewLevel, err := ParseEscalationLevel(review)
if err != nil {
return RoutingPolicy{}, fmt.Errorf("review escalation: %w", err)
}
return RoutingPolicy{
Fast:    RolePolicy{Escalation: fastLevel},
Default: RolePolicy{Escalation: defaultLvl},
Planner: RolePolicy{Escalation: plannerLevel},
Review:  RolePolicy{Escalation: reviewLevel},
}, nil
}
