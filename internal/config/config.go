package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all configuration for the Runeforge platform.
type Config struct {
	Database    DatabaseConfig
	NATS        NATSConfig
	HTTP        HTTPConfig
	Auth        AuthConfig
	Logging     LoggingConfig
	Providers   ProvidersConfig
	Features    FeatureFlags
	Retry       RetryConfig
	Telegram    TelegramConfig
	GoogleTasks GoogleTasksConfig
	Routing     RoutingPolicyConfig
}

type DatabaseConfig struct {
	DSN      string `envconfig:"DATABASE_DSN" required:"true"`
	MaxConns int32  `envconfig:"DATABASE_MAX_CONNS" default:"25"`
}

type NATSConfig struct {
	URL          string `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	StreamPrefix string `envconfig:"NATS_STREAM_PREFIX" default:"runeforge"`
}

type HTTPConfig struct {
	Port            int           `envconfig:"HTTP_PORT" default:"8080"`
	ReadTimeout     time.Duration `envconfig:"HTTP_READ_TIMEOUT" default:"30s"`
	WriteTimeout    time.Duration `envconfig:"HTTP_WRITE_TIMEOUT" default:"30s"`
	ShutdownTimeout time.Duration `envconfig:"HTTP_SHUTDOWN_TIMEOUT" default:"15s"`
}

type AuthConfig struct {
	AdminToken string `envconfig:"ADMIN_TOKEN" required:"true"`
}

type LoggingConfig struct {
	Level  string `envconfig:"LOG_LEVEL" default:"info"`
	Format string `envconfig:"LOG_FORMAT" default:"json"`
}

type ProvidersConfig struct {
	OpenAI      OpenAIConfig
	OpenRouter  OpenRouterConfig
	Ollama      OllamaConfig
	OllamaCloud OllamaCloudConfig
}

type OpenAIConfig struct {
	APIKey         string `envconfig:"OPENAI_API_KEY"`
	BaseURL        string `envconfig:"OPENAI_BASE_URL" default:"https://api.openai.com/v1"`
	DefaultModel   string `envconfig:"OPENAI_DEFAULT_MODEL" default:"gpt-4o-mini"`
	TimeoutSeconds int    `envconfig:"OPENAI_TIMEOUT_SECONDS" default:"60"`
	Timeout        time.Duration
}

// OpenRouterConfig holds configuration for the OpenRouter AI gateway backend.
// When Enabled is false, no OpenRouter provider is instantiated and profile
// candidates with provider=openrouter fall back to the primary provider with a warning.
type OpenRouterConfig struct {
	// Enabled gates the OpenRouter provider. Default false — safe for local-only setups.
	Enabled bool `envconfig:"OPENROUTER_ENABLED" default:"false"`
	// APIKey is the Bearer token for OpenRouter authentication. Required when Enabled.
	APIKey string `envconfig:"OPENROUTER_API_KEY"`
	// BaseURL is the OpenRouter API endpoint.
	BaseURL string `envconfig:"OPENROUTER_BASE_URL" default:"https://openrouter.ai/api/v1"`
	// DefaultModel is the fallback model when no model is specified per-candidate.
	DefaultModel string `envconfig:"OPENROUTER_DEFAULT_MODEL" default:"openai/gpt-4o-mini"`
	// TimeoutSeconds is the default per-request timeout for OpenRouter calls.
	TimeoutSeconds int `envconfig:"OPENROUTER_TIMEOUT_SECONDS" default:"60"`
	// HTTPReferer is sent as the HTTP-Referer header for OpenRouter attribution (optional).
	HTTPReferer string `envconfig:"OPENROUTER_HTTP_REFERER"`
	// AppName is sent as the X-Title header for OpenRouter attribution (optional).
	AppName string `envconfig:"OPENROUTER_APP_NAME"`
	// Computed.
	Timeout time.Duration
}

type OllamaConfig struct {
	BaseURL      string `envconfig:"OLLAMA_BASE_URL" default:"http://localhost:11434"`
	DefaultModel string `envconfig:"OLLAMA_DEFAULT_MODEL" default:"llama3.2"`
	FastModel    string `envconfig:"OLLAMA_FAST_MODEL"`
	PlannerModel string `envconfig:"OLLAMA_PLANNER_MODEL"`
	ReviewModel  string `envconfig:"OLLAMA_REVIEW_MODEL"`

	TimeoutSeconds        int `envconfig:"OLLAMA_TIMEOUT_SECONDS" default:"120"`
	FastTimeoutSeconds    int `envconfig:"OLLAMA_FAST_TIMEOUT_SECONDS"`
	PlannerTimeoutSeconds int `envconfig:"OLLAMA_PLANNER_TIMEOUT_SECONDS"`

	DefaultProfile string `envconfig:"MODEL_DEFAULT_PROFILE"`
	FastProfile    string `envconfig:"MODEL_FAST_PROFILE"`
	PlannerProfile string `envconfig:"MODEL_PLANNER_PROFILE"`
	ReviewProfile  string `envconfig:"MODEL_REVIEW_PROFILE"`

	// Computed duration fields (derived from *Seconds fields in Load).
	Timeout        time.Duration
	FastTimeout    time.Duration
	PlannerTimeout time.Duration
}

// OllamaCloudConfig holds configuration for the Ollama Cloud backend.
// When Enabled is false, no cloud provider is instantiated and
// profile candidates with provider=ollama-cloud fall back to the primary
// provider with a warning.
type OllamaCloudConfig struct {
	// Enabled gates the cloud provider. Default false — safe for local-only setups.
	Enabled bool `envconfig:"OLLAMA_CLOUD_ENABLED" default:"false"`
	// BaseURL is the Ollama Cloud API endpoint, e.g. "https://cloud.ollama.ai".
	BaseURL string `envconfig:"OLLAMA_CLOUD_BASE_URL"`
	// APIKey is the Bearer token for Ollama Cloud authentication.
	APIKey string `envconfig:"OLLAMA_CLOUD_API_KEY"`
	// TimeoutSeconds is the default per-request timeout for cloud calls.
	TimeoutSeconds int `envconfig:"OLLAMA_CLOUD_TIMEOUT_SECONDS" default:"120"`
	// Computed.
	Timeout time.Duration
}

type FeatureFlags struct {
	AutoApprove      bool `envconfig:"FEATURE_AUTO_APPROVE" default:"false"`
	WritebackEnabled bool `envconfig:"FEATURE_WRITEBACK_ENABLED" default:"false"`
}

type RetryConfig struct {
	MaxAttempts            int     `envconfig:"RETRY_MAX_ATTEMPTS" default:"3"`
	BackoffMultiplier      float64 `envconfig:"RETRY_BACKOFF_MULTIPLIER" default:"2.0"`
	InitialIntervalSeconds int     `envconfig:"RETRY_INITIAL_INTERVAL_SECONDS" default:"5"`
}

type TelegramConfig struct {
	BotToken    string `envconfig:"TELEGRAM_BOT_TOKEN"`
	OwnerChatID int64  `envconfig:"TELEGRAM_OWNER_CHAT_ID"`
}

type GoogleTasksConfig struct {
	CredentialsPath     string `envconfig:"GOOGLE_TASKS_CREDENTIALS_PATH"`
	PollIntervalSeconds int    `envconfig:"GOOGLE_TASKS_POLL_INTERVAL_SECONDS" default:"300"`
}

// RoutingPolicyConfig holds the explicit routing policy for model/provider selection.
// Per-role escalation levels control how far a role may escalate through provider tiers:
// local (cheapest/fastest) → cloud → OpenRouter (most capable/costly).
//
// Explicit DSL profile overrides (MODEL_*_PROFILE env vars) always take precedence over policy.
type RoutingPolicyConfig struct {
	// Per-role escalation levels.
	// Valid values: local_only, local_cloud, local_cloud_openrouter, local_openrouter.
	FastEscalation    string `envconfig:"ROUTING_FAST_ESCALATION" default:"local_only"`
	DefaultEscalation string `envconfig:"ROUTING_DEFAULT_ESCALATION" default:"local_only"`
	PlannerEscalation string `envconfig:"ROUTING_PLANNER_ESCALATION" default:"local_cloud"`
	ReviewEscalation  string `envconfig:"ROUTING_REVIEW_ESCALATION" default:"local_cloud"`

	// CloudModel is the model name to request from Ollama Cloud when escalating.
	// If empty, the resolver uses the role's local model (same model family, cloud tier).
	CloudModel string `envconfig:"ROUTING_CLOUD_MODEL"`

	// OpenRouterModel is the model name to request from OpenRouter when escalating.
	// If empty, falls back to OPENROUTER_DEFAULT_MODEL.
	// Must resolve to a non-empty value if any role permits OpenRouter escalation
	// and OPENROUTER_ENABLED=true.
	OpenRouterModel string `envconfig:"ROUTING_OPENROUTER_MODEL"`
}

// applyProfileBackcompat copies deprecated OLLAMA_*_PROFILE env vars into the
// new MODEL_*_PROFILE fields when the latter are unset. It returns one warning
// string per deprecated var consumed so the caller can surface them.
func applyProfileBackcompat(cfg *OllamaConfig) []string {
	type mapping struct {
		oldKey  string
		newKey  string
		current *string
	}
	pairs := []mapping{
		{"OLLAMA_DEFAULT_PROFILE", "MODEL_DEFAULT_PROFILE", &cfg.DefaultProfile},
		{"OLLAMA_FAST_PROFILE", "MODEL_FAST_PROFILE", &cfg.FastProfile},
		{"OLLAMA_PLANNER_PROFILE", "MODEL_PLANNER_PROFILE", &cfg.PlannerProfile},
		{"OLLAMA_REVIEW_PROFILE", "MODEL_REVIEW_PROFILE", &cfg.ReviewProfile},
	}
	var warnings []string
	for _, p := range pairs {
		if *p.current == "" {
			if v := os.Getenv(p.oldKey); v != "" {
				*p.current = v
				warnings = append(warnings, fmt.Sprintf("%s is deprecated; rename to %s", p.oldKey, p.newKey))
			}
		}
	}
	return warnings
}

// Load reads configuration from environment variables using envconfig.
// It fails fast on invalid required fields.
func Load() (*Config, error) {
	var cfg Config

	if err := envconfig.Process("", &cfg.Database); err != nil {
		return nil, fmt.Errorf("database config: %w", err)
	}
	if err := envconfig.Process("", &cfg.NATS); err != nil {
		return nil, fmt.Errorf("nats config: %w", err)
	}
	if err := envconfig.Process("", &cfg.HTTP); err != nil {
		return nil, fmt.Errorf("http config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Auth); err != nil {
		return nil, fmt.Errorf("auth config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Logging); err != nil {
		return nil, fmt.Errorf("logging config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Providers.OpenAI); err != nil {
		return nil, fmt.Errorf("openai config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Providers.OpenRouter); err != nil {
		return nil, fmt.Errorf("openrouter config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Providers.Ollama); err != nil {
		return nil, fmt.Errorf("ollama config: %w", err)
	}
	for _, w := range applyProfileBackcompat(&cfg.Providers.Ollama) {
		fmt.Fprintf(os.Stderr, "[config] DEPRECATED: %s\n", w)
	}
	if err := envconfig.Process("", &cfg.Providers.OllamaCloud); err != nil {
		return nil, fmt.Errorf("ollama cloud config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Features); err != nil {
		return nil, fmt.Errorf("features config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Retry); err != nil {
		return nil, fmt.Errorf("retry config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Telegram); err != nil {
		return nil, fmt.Errorf("telegram config: %w", err)
	}
	if err := envconfig.Process("", &cfg.GoogleTasks); err != nil {
		return nil, fmt.Errorf("google tasks config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Routing); err != nil {
		return nil, fmt.Errorf("routing policy config: %w", err)
	}

	// Derive computed fields
	cfg.Providers.OpenAI.Timeout = time.Duration(cfg.Providers.OpenAI.TimeoutSeconds) * time.Second
	cfg.Providers.OpenRouter.Timeout = time.Duration(cfg.Providers.OpenRouter.TimeoutSeconds) * time.Second
	cfg.Providers.Ollama.Timeout = time.Duration(cfg.Providers.Ollama.TimeoutSeconds) * time.Second
	if cfg.Providers.Ollama.FastTimeoutSeconds > 0 {
		cfg.Providers.Ollama.FastTimeout = time.Duration(cfg.Providers.Ollama.FastTimeoutSeconds) * time.Second
	} else {
		cfg.Providers.Ollama.FastTimeout = cfg.Providers.Ollama.Timeout
	}
	if cfg.Providers.Ollama.PlannerTimeoutSeconds > 0 {
		cfg.Providers.Ollama.PlannerTimeout = time.Duration(cfg.Providers.Ollama.PlannerTimeoutSeconds) * time.Second
	} else {
		cfg.Providers.Ollama.PlannerTimeout = cfg.Providers.Ollama.Timeout
	}
	cfg.Providers.OllamaCloud.Timeout = time.Duration(cfg.Providers.OllamaCloud.TimeoutSeconds) * time.Second

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

// validate performs semantic validation beyond envconfig's required checks.
func (c *Config) validate() error {
	var errs []string

	if c.Database.MaxConns <= 0 {
		errs = append(errs, "DATABASE_MAX_CONNS must be greater than 0")
	}
	if c.Retry.MaxAttempts <= 0 {
		errs = append(errs, "RETRY_MAX_ATTEMPTS must be greater than 0")
	}
	if c.Retry.BackoffMultiplier <= 0 {
		errs = append(errs, "RETRY_BACKOFF_MULTIPLIER must be greater than 0")
	}
	if c.Providers.Ollama.TimeoutSeconds <= 0 {
		errs = append(errs, "OLLAMA_TIMEOUT_SECONDS must be greater than 0")
	}
	if c.Providers.Ollama.FastTimeoutSeconds < 0 {
		errs = append(errs, "OLLAMA_FAST_TIMEOUT_SECONDS must be non-negative")
	}
	if c.Providers.Ollama.PlannerTimeoutSeconds < 0 {
		errs = append(errs, "OLLAMA_PLANNER_TIMEOUT_SECONDS must be non-negative")
	}
	if c.Providers.OllamaCloud.Enabled && c.Providers.OllamaCloud.BaseURL == "" {
		errs = append(errs, "OLLAMA_CLOUD_BASE_URL is required when OLLAMA_CLOUD_ENABLED is true")
	}
	if c.Providers.OllamaCloud.TimeoutSeconds <= 0 {
		errs = append(errs, "OLLAMA_CLOUD_TIMEOUT_SECONDS must be greater than 0")
	}
	if c.Providers.OpenRouter.Enabled && c.Providers.OpenRouter.APIKey == "" {
		errs = append(errs, "OPENROUTER_API_KEY is required when OPENROUTER_ENABLED is true")
	}
	if c.Providers.OpenRouter.TimeoutSeconds <= 0 {
		errs = append(errs, "OPENROUTER_TIMEOUT_SECONDS must be greater than 0")
	}

	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(c.Logging.Level)] {
		errs = append(errs, fmt.Sprintf("LOG_LEVEL must be one of: debug, info, warn, error; got %q", c.Logging.Level))
	}

	validFormats := map[string]bool{"json": true, "console": true}
	if !validFormats[strings.ToLower(c.Logging.Format)] {
		errs = append(errs, fmt.Sprintf("LOG_FORMAT must be one of: json, console; got %q", c.Logging.Format))
	}

	// Validate routing escalation levels at startup so misconfiguration fails fast.
	validEscLevels := map[string]bool{
		"local_only": true, "local": true,
		"local_cloud":            true,
		"local_cloud_openrouter": true, "full": true,
		"local_openrouter": true,
	}
	for envName, level := range map[string]string{
		"ROUTING_FAST_ESCALATION":    c.Routing.FastEscalation,
		"ROUTING_DEFAULT_ESCALATION": c.Routing.DefaultEscalation,
		"ROUTING_PLANNER_ESCALATION": c.Routing.PlannerEscalation,
		"ROUTING_REVIEW_ESCALATION":  c.Routing.ReviewEscalation,
	} {
		if !validEscLevels[strings.ToLower(strings.TrimSpace(level))] {
			errs = append(errs, fmt.Sprintf(
				"%s must be one of local_only, local_cloud, local_cloud_openrouter, local_openrouter; got %q",
				envName, level,
			))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// ResolveModel returns the configured model name for the given role.
// If a role-specific model is not configured, it falls back to DefaultModel.
func (c *OllamaConfig) ResolveModel(role string) string {
	switch role {
	case "fast":
		if c.FastModel != "" {
			return c.FastModel
		}
	case "planner":
		if c.PlannerModel != "" {
			return c.PlannerModel
		}
	case "review":
		if c.ReviewModel != "" {
			return c.ReviewModel
		}
	}
	return c.DefaultModel
}

// ResolveTimeout returns the configured timeout for the given role.
// If a role-specific timeout is not configured, it falls back to Timeout.
func (c *OllamaConfig) ResolveTimeout(role string) time.Duration {
	switch role {
	case "fast":
		if c.FastTimeoutSeconds > 0 {
			return c.FastTimeout
		}
	case "planner":
		if c.PlannerTimeoutSeconds > 0 {
			return c.PlannerTimeout
		}
	}
	// "review" and "default" both use the base timeout.
	return c.Timeout
}
