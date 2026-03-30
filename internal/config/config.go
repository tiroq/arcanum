package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all configuration for the Runeforge platform.
type Config struct {
	Database     DatabaseConfig
	NATS         NATSConfig
	HTTP         HTTPConfig
	Auth         AuthConfig
	Logging      LoggingConfig
	Providers    ProvidersConfig
	Features     FeatureFlags
	Retry        RetryConfig
	GoogleTasks  GoogleTasksConfig
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
	OpenAI     OpenAIConfig
	OpenRouter OpenRouterConfig
	Ollama     OllamaConfig
}

type OpenAIConfig struct {
	APIKey         string        `envconfig:"OPENAI_API_KEY"`
	BaseURL        string        `envconfig:"OPENAI_BASE_URL" default:"https://api.openai.com/v1"`
	DefaultModel   string        `envconfig:"OPENAI_DEFAULT_MODEL" default:"gpt-4o-mini"`
	TimeoutSeconds int           `envconfig:"OPENAI_TIMEOUT_SECONDS" default:"60"`
	Timeout        time.Duration
}

type OpenRouterConfig struct {
	APIKey         string        `envconfig:"OPENROUTER_API_KEY"`
	BaseURL        string        `envconfig:"OPENROUTER_BASE_URL" default:"https://openrouter.ai/api/v1"`
	DefaultModel   string        `envconfig:"OPENROUTER_DEFAULT_MODEL" default:"openai/gpt-4o-mini"`
	TimeoutSeconds int           `envconfig:"OPENROUTER_TIMEOUT_SECONDS" default:"60"`
	Timeout        time.Duration
}

type OllamaConfig struct {
	BaseURL        string        `envconfig:"OLLAMA_BASE_URL" default:"http://localhost:11434"`
	DefaultModel   string        `envconfig:"OLLAMA_DEFAULT_MODEL" default:"llama3.2"`
	TimeoutSeconds int           `envconfig:"OLLAMA_TIMEOUT_SECONDS" default:"120"`
	Timeout        time.Duration
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

type GoogleTasksConfig struct {
	CredentialsPath     string `envconfig:"GOOGLE_TASKS_CREDENTIALS_PATH"`
	PollIntervalSeconds int    `envconfig:"GOOGLE_TASKS_POLL_INTERVAL_SECONDS" default:"300"`
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
	if err := envconfig.Process("", &cfg.Features); err != nil {
		return nil, fmt.Errorf("features config: %w", err)
	}
	if err := envconfig.Process("", &cfg.Retry); err != nil {
		return nil, fmt.Errorf("retry config: %w", err)
	}
	if err := envconfig.Process("", &cfg.GoogleTasks); err != nil {
		return nil, fmt.Errorf("google tasks config: %w", err)
	}

	// Derive computed fields
	cfg.Providers.OpenAI.Timeout = time.Duration(cfg.Providers.OpenAI.TimeoutSeconds) * time.Second
	cfg.Providers.OpenRouter.Timeout = time.Duration(cfg.Providers.OpenRouter.TimeoutSeconds) * time.Second
	cfg.Providers.Ollama.Timeout = time.Duration(cfg.Providers.Ollama.TimeoutSeconds) * time.Second

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

	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(c.Logging.Level)] {
		errs = append(errs, fmt.Sprintf("LOG_LEVEL must be one of: debug, info, warn, error; got %q", c.Logging.Level))
	}

	validFormats := map[string]bool{"json": true, "console": true}
	if !validFormats[strings.ToLower(c.Logging.Format)] {
		errs = append(errs, fmt.Sprintf("LOG_FORMAT must be one of: json, console; got %q", c.Logging.Format))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
