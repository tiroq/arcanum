package providers

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestOpenAIProviderName(t *testing.T) {
	p := NewOpenAIProvider("openai", "https://api.openai.com/v1", "key", 30*time.Second, zap.NewNop())
	if p.Name() != "openai" {
		t.Errorf("expected 'openai', got %q", p.Name())
	}
}

func TestOllamaProviderName(t *testing.T) {
	p := NewOllamaProvider("ollama", "http://localhost:11434", 30*time.Second, zap.NewNop())
	if p.Name() != "ollama" {
		t.Errorf("expected 'ollama', got %q", p.Name())
	}
}

func TestProviderRegistryGetMissing(t *testing.T) {
	r := NewProviderRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing provider, got nil")
	}
}

func TestProviderRegistryRegisterAndGet(t *testing.T) {
	r := NewProviderRegistry()
	p := NewOllamaProvider("local", "http://localhost:11434", 30*time.Second, zap.NewNop())
	r.Register("local", p)

	got, err := r.Get("local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "local" {
		t.Errorf("expected 'local', got %q", got.Name())
	}
}
