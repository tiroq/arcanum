package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiroq/arcanum/internal/config"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_DSN", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("ADMIN_TOKEN", "test-admin-token")
}

func TestConfigDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, int32(25), cfg.Database.MaxConns)
	assert.Equal(t, "nats://localhost:4222", cfg.NATS.URL)
	assert.Equal(t, "runeforge", cfg.NATS.StreamPrefix)
	assert.Equal(t, 8080, cfg.HTTP.Port)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Providers.OpenAI.BaseURL)
	assert.Equal(t, "gpt-4o-mini", cfg.Providers.OpenAI.DefaultModel)
	assert.Equal(t, "https://openrouter.ai/api/v1", cfg.Providers.OpenRouter.BaseURL)
	assert.Equal(t, "http://localhost:11434", cfg.Providers.Ollama.BaseURL)
	assert.Equal(t, false, cfg.Features.AutoApprove)
	assert.Equal(t, false, cfg.Features.WritebackEnabled)
	assert.Equal(t, 3, cfg.Retry.MaxAttempts)
	assert.Equal(t, 2.0, cfg.Retry.BackoffMultiplier)
	assert.Equal(t, 5, cfg.Retry.InitialIntervalSeconds)
}

func TestConfigValidation_MissingRequired(t *testing.T) {
	// Ensure DATABASE_DSN is not set
	os.Unsetenv("DATABASE_DSN")
	os.Unsetenv("ADMIN_TOKEN")

	_, err := config.Load()
	require.Error(t, err)
}

func TestConfigValidation_InvalidLogLevel(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LOG_LEVEL", "verbose")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
}

func TestConfigValidation_InvalidLogFormat(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LOG_FORMAT", "xml")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_FORMAT")
}

func TestConfigValidation_InvalidMaxConns(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_MAX_CONNS", "0")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_MAX_CONNS")
}

func TestOllamaMultiModelDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "llama3.2", cfg.Providers.Ollama.DefaultModel)
	assert.Empty(t, cfg.Providers.Ollama.FastModel)
	assert.Empty(t, cfg.Providers.Ollama.PlannerModel)
	assert.Empty(t, cfg.Providers.Ollama.ReviewModel)
	assert.Equal(t, 120, cfg.Providers.Ollama.TimeoutSeconds)
	assert.Equal(t, 0, cfg.Providers.Ollama.FastTimeoutSeconds)
	assert.Equal(t, 0, cfg.Providers.Ollama.PlannerTimeoutSeconds)
	assert.Empty(t, cfg.Providers.Ollama.DefaultProfile)
	assert.Empty(t, cfg.Providers.Ollama.FastProfile)
	assert.Empty(t, cfg.Providers.Ollama.PlannerProfile)
	assert.Empty(t, cfg.Providers.Ollama.ReviewProfile)
}

func TestOllamaMultiModelOverrides(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OLLAMA_DEFAULT_MODEL", "qwen2.5:7b-instruct")
	t.Setenv("OLLAMA_FAST_MODEL", "llama3.2:3b")
	t.Setenv("OLLAMA_PLANNER_MODEL", "qwen2.5:14b-instruct")
	t.Setenv("OLLAMA_REVIEW_MODEL", "qwen2.5:7b-instruct")
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "180")
	t.Setenv("OLLAMA_FAST_TIMEOUT_SECONDS", "90")
	t.Setenv("OLLAMA_PLANNER_TIMEOUT_SECONDS", "240")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "qwen2.5:7b-instruct", cfg.Providers.Ollama.DefaultModel)
	assert.Equal(t, "llama3.2:3b", cfg.Providers.Ollama.FastModel)
	assert.Equal(t, "qwen2.5:14b-instruct", cfg.Providers.Ollama.PlannerModel)
	assert.Equal(t, "qwen2.5:7b-instruct", cfg.Providers.Ollama.ReviewModel)
	assert.Equal(t, 180, cfg.Providers.Ollama.TimeoutSeconds)
	assert.Equal(t, 90, cfg.Providers.Ollama.FastTimeoutSeconds)
	assert.Equal(t, 240, cfg.Providers.Ollama.PlannerTimeoutSeconds)
}

func TestOllamaTimeoutDerivation(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "180")
	t.Setenv("OLLAMA_FAST_TIMEOUT_SECONDS", "90")
	t.Setenv("OLLAMA_PLANNER_TIMEOUT_SECONDS", "240")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 180*time.Second, cfg.Providers.Ollama.Timeout)
	assert.Equal(t, 90*time.Second, cfg.Providers.Ollama.FastTimeout)
	assert.Equal(t, 240*time.Second, cfg.Providers.Ollama.PlannerTimeout)
}

func TestOllamaTimeoutFallback(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "180")
	// Do not set FAST or PLANNER timeout -> should fallback to default

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 180*time.Second, cfg.Providers.Ollama.FastTimeout)
	assert.Equal(t, 180*time.Second, cfg.Providers.Ollama.PlannerTimeout)
}

func TestOllamaResolveModel(t *testing.T) {
	ollamaCfg := config.OllamaConfig{
		DefaultModel: "qwen2.5:7b-instruct",
		FastModel:    "llama3.2:3b",
		PlannerModel: "qwen2.5:14b-instruct",
	}

	assert.Equal(t, "qwen2.5:7b-instruct", ollamaCfg.ResolveModel("default"))
	assert.Equal(t, "llama3.2:3b", ollamaCfg.ResolveModel("fast"))
	assert.Equal(t, "qwen2.5:14b-instruct", ollamaCfg.ResolveModel("planner"))
	// review not set, should fallback
	assert.Equal(t, "qwen2.5:7b-instruct", ollamaCfg.ResolveModel("review"))
	// unknown role falls back
	assert.Equal(t, "qwen2.5:7b-instruct", ollamaCfg.ResolveModel("unknown"))
}

func TestOllamaResolveTimeout(t *testing.T) {
	ollamaCfg := config.OllamaConfig{
		TimeoutSeconds:        180,
		Timeout:               180 * time.Second,
		FastTimeoutSeconds:    90,
		FastTimeout:           90 * time.Second,
		PlannerTimeoutSeconds: 240,
		PlannerTimeout:        240 * time.Second,
	}

	assert.Equal(t, 180*time.Second, ollamaCfg.ResolveTimeout("default"))
	assert.Equal(t, 90*time.Second, ollamaCfg.ResolveTimeout("fast"))
	assert.Equal(t, 240*time.Second, ollamaCfg.ResolveTimeout("planner"))
	assert.Equal(t, 180*time.Second, ollamaCfg.ResolveTimeout("review"))
}

func TestOllamaProfileEnvVars(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MODEL_FAST_PROFILE", "model-a?think=thinking&timeout=120|model-b?timeout=60")
	t.Setenv("MODEL_PLANNER_PROFILE", "planner-model?think=nothinking&timeout=300")
	t.Setenv("MODEL_REVIEW_PROFILE", "review-a|review-b?json=true")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "model-a?think=thinking&timeout=120|model-b?timeout=60", cfg.Providers.Ollama.FastProfile)
	assert.Equal(t, "planner-model?think=nothinking&timeout=300", cfg.Providers.Ollama.PlannerProfile)
	assert.Equal(t, "review-a|review-b?json=true", cfg.Providers.Ollama.ReviewProfile)
	assert.Empty(t, cfg.Providers.Ollama.DefaultProfile)
}

func TestOllamaProfileBackcompat(t *testing.T) {
	setRequiredEnv(t)
	// Set only the deprecated OLLAMA_*_PROFILE vars — MODEL_*_PROFILE are unset.
	t.Setenv("OLLAMA_DEFAULT_PROFILE", "compat-default?think=off&timeout=90")
	t.Setenv("OLLAMA_FAST_PROFILE", "compat-fast?timeout=30")
	t.Setenv("OLLAMA_PLANNER_PROFILE", "compat-planner?think=on&timeout=240")
	t.Setenv("OLLAMA_REVIEW_PROFILE", "compat-review?json=true")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "compat-default?think=off&timeout=90", cfg.Providers.Ollama.DefaultProfile)
	assert.Equal(t, "compat-fast?timeout=30", cfg.Providers.Ollama.FastProfile)
	assert.Equal(t, "compat-planner?think=on&timeout=240", cfg.Providers.Ollama.PlannerProfile)
	assert.Equal(t, "compat-review?json=true", cfg.Providers.Ollama.ReviewProfile)
}

func TestOllamaProfileNewTakesPrecedenceOverOld(t *testing.T) {
	setRequiredEnv(t)
	// New MODEL_*_PROFILE must win over deprecated OLLAMA_*_PROFILE.
	t.Setenv("MODEL_FAST_PROFILE", "new-fast?timeout=60")
	t.Setenv("OLLAMA_FAST_PROFILE", "old-fast?timeout=30")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "new-fast?timeout=60", cfg.Providers.Ollama.FastProfile)
}

func TestOllamaCloudDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.False(t, cfg.Providers.OllamaCloud.Enabled, "cloud must be disabled by default")
	assert.Empty(t, cfg.Providers.OllamaCloud.BaseURL)
	assert.Empty(t, cfg.Providers.OllamaCloud.APIKey)
	assert.Equal(t, 120, cfg.Providers.OllamaCloud.TimeoutSeconds)
	assert.Equal(t, 120*time.Second, cfg.Providers.OllamaCloud.Timeout)
}

func TestOllamaCloudTimeoutDerivation(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OLLAMA_CLOUD_ENABLED", "true")
	t.Setenv("OLLAMA_CLOUD_BASE_URL", "https://cloud.ollama.ai")
	t.Setenv("OLLAMA_CLOUD_TIMEOUT_SECONDS", "240")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 240*time.Second, cfg.Providers.OllamaCloud.Timeout)
}

func TestOllamaCloudValidation_EnabledWithoutURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OLLAMA_CLOUD_ENABLED", "true")
	// BaseURL intentionally left empty

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OLLAMA_CLOUD_BASE_URL")
}

func TestOllamaCloudValidation_DisabledNoURLNeeded(t *testing.T) {
	setRequiredEnv(t)
	// Enabled=false (default) — empty URL must not cause a validation error

	_, err := config.Load()
	require.NoError(t, err)
}

func TestOllamaCloudFullConfig(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OLLAMA_CLOUD_ENABLED", "true")
	t.Setenv("OLLAMA_CLOUD_BASE_URL", "https://cloud.ollama.ai")
	t.Setenv("OLLAMA_CLOUD_API_KEY", "test-secret-key")
	t.Setenv("OLLAMA_CLOUD_TIMEOUT_SECONDS", "180")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.True(t, cfg.Providers.OllamaCloud.Enabled)
	assert.Equal(t, "https://cloud.ollama.ai", cfg.Providers.OllamaCloud.BaseURL)
	assert.Equal(t, "test-secret-key", cfg.Providers.OllamaCloud.APIKey)
	assert.Equal(t, 180, cfg.Providers.OllamaCloud.TimeoutSeconds)
	assert.Equal(t, 180*time.Second, cfg.Providers.OllamaCloud.Timeout)
}

func TestOpenRouterDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.False(t, cfg.Providers.OpenRouter.Enabled, "openrouter must be disabled by default")
	assert.Equal(t, "https://openrouter.ai/api/v1", cfg.Providers.OpenRouter.BaseURL)
	assert.Equal(t, "openai/gpt-4o-mini", cfg.Providers.OpenRouter.DefaultModel)
	assert.Empty(t, cfg.Providers.OpenRouter.APIKey)
	assert.Equal(t, 60, cfg.Providers.OpenRouter.TimeoutSeconds)
	assert.Equal(t, 60*time.Second, cfg.Providers.OpenRouter.Timeout)
	assert.Empty(t, cfg.Providers.OpenRouter.HTTPReferer)
	assert.Empty(t, cfg.Providers.OpenRouter.AppName)
}

func TestOpenRouterTimeoutDerivation(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OPENROUTER_ENABLED", "true")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test-key")
	t.Setenv("OPENROUTER_TIMEOUT_SECONDS", "90")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 90*time.Second, cfg.Providers.OpenRouter.Timeout)
}

func TestOpenRouterValidation_EnabledWithoutAPIKey(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OPENROUTER_ENABLED", "true")
	// APIKey intentionally left empty

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OPENROUTER_API_KEY")
}

func TestOpenRouterValidation_DisabledNoKeyNeeded(t *testing.T) {
	setRequiredEnv(t)
	// Enabled=false (default) — empty API key must not cause a validation error

	_, err := config.Load()
	require.NoError(t, err)
}

func TestOpenRouterFullConfig(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OPENROUTER_ENABLED", "true")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-secret-key")
	t.Setenv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
	t.Setenv("OPENROUTER_TIMEOUT_SECONDS", "120")
	t.Setenv("OPENROUTER_HTTP_REFERER", "https://arcanum.example.com")
	t.Setenv("OPENROUTER_APP_NAME", "Arcanum")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.True(t, cfg.Providers.OpenRouter.Enabled)
	assert.Equal(t, "sk-or-secret-key", cfg.Providers.OpenRouter.APIKey)
	assert.Equal(t, "https://openrouter.ai/api/v1", cfg.Providers.OpenRouter.BaseURL)
	assert.Equal(t, 120, cfg.Providers.OpenRouter.TimeoutSeconds)
	assert.Equal(t, 120*time.Second, cfg.Providers.OpenRouter.Timeout)
	assert.Equal(t, "https://arcanum.example.com", cfg.Providers.OpenRouter.HTTPReferer)
	assert.Equal(t, "Arcanum", cfg.Providers.OpenRouter.AppName)
}
