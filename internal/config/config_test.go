package config_test

import (
	"os"
	"testing"

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
