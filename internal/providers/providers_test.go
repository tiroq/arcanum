package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/config"
)

func TestOpenAIProviderName(t *testing.T) {
	p := NewOpenAIProvider("openai", "https://api.openai.com/v1", "key", 30*time.Second, zap.NewNop())
	if p.Name() != "openai" {
		t.Errorf("expected 'openai', got %q", p.Name())
	}
}

func TestOllamaProviderName(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:        "http://localhost:11434",
		DefaultModel:   "llama3.2",
		TimeoutSeconds: 120,
		Timeout:        120 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())
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
	cfg := config.OllamaConfig{
		BaseURL:        "http://localhost:11434",
		DefaultModel:   "llama3.2",
		TimeoutSeconds: 120,
		Timeout:        120 * time.Second,
	}
	p := NewOllamaProvider("local", cfg, zap.NewNop())
	r.Register("local", p)

	got, err := r.Get("local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "local" {
		t.Errorf("expected 'local', got %q", got.Name())
	}
}

func TestOllamaResolveModel_DefaultRole(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:      "http://localhost:11434",
		DefaultModel: "qwen2.5:7b-instruct",
		Timeout:      180 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	if got := p.ResolveModel(RoleDefault); got != "qwen2.5:7b-instruct" {
		t.Errorf("expected default model, got %q", got)
	}
}

func TestOllamaResolveModel_FastRole(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:      "http://localhost:11434",
		DefaultModel: "qwen2.5:7b-instruct",
		FastModel:    "llama3.2:3b",
		Timeout:      180 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	if got := p.ResolveModel(RoleFast); got != "llama3.2:3b" {
		t.Errorf("expected fast model 'llama3.2:3b', got %q", got)
	}
}

func TestOllamaResolveModel_PlannerRole(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:      "http://localhost:11434",
		DefaultModel: "qwen2.5:7b-instruct",
		PlannerModel: "qwen2.5:14b-instruct",
		Timeout:      180 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	if got := p.ResolveModel(RolePlanner); got != "qwen2.5:14b-instruct" {
		t.Errorf("expected planner model, got %q", got)
	}
}

func TestOllamaResolveModel_ReviewRole(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:      "http://localhost:11434",
		DefaultModel: "qwen2.5:7b-instruct",
		ReviewModel:  "qwen2.5:7b-instruct",
		Timeout:      180 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	if got := p.ResolveModel(RoleReview); got != "qwen2.5:7b-instruct" {
		t.Errorf("expected review model, got %q", got)
	}
}

func TestOllamaResolveModel_FallbackToDefault(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:      "http://localhost:11434",
		DefaultModel: "qwen2.5:7b-instruct",
		Timeout:      180 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	for _, role := range []ModelRole{RoleFast, RolePlanner, RoleReview} {
		if got := p.ResolveModel(role); got != "qwen2.5:7b-instruct" {
			t.Errorf("role %q: expected fallback to default model, got %q", role, got)
		}
	}
}

func TestOllamaResolveTimeout_DefaultRole(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:        "http://localhost:11434",
		DefaultModel:   "qwen2.5:7b-instruct",
		TimeoutSeconds: 180,
		Timeout:        180 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	if got := p.ResolveTimeout(RoleDefault); got != 180*time.Second {
		t.Errorf("expected 180s, got %v", got)
	}
}

func TestOllamaResolveTimeout_FastRole(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:            "http://localhost:11434",
		DefaultModel:       "qwen2.5:7b-instruct",
		TimeoutSeconds:     180,
		Timeout:            180 * time.Second,
		FastTimeoutSeconds: 90,
		FastTimeout:        90 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	if got := p.ResolveTimeout(RoleFast); got != 90*time.Second {
		t.Errorf("expected 90s, got %v", got)
	}
}

func TestOllamaResolveTimeout_PlannerRole(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:               "http://localhost:11434",
		DefaultModel:          "qwen2.5:7b-instruct",
		TimeoutSeconds:        180,
		Timeout:               180 * time.Second,
		PlannerTimeoutSeconds: 240,
		PlannerTimeout:        240 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	if got := p.ResolveTimeout(RolePlanner); got != 240*time.Second {
		t.Errorf("expected 240s, got %v", got)
	}
}

func TestOllamaResolveTimeout_FallbackToDefault(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:        "http://localhost:11434",
		DefaultModel:   "qwen2.5:7b-instruct",
		TimeoutSeconds: 180,
		Timeout:        180 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	for _, role := range []ModelRole{RoleFast, RolePlanner, RoleReview} {
		if got := p.ResolveTimeout(role); got != 180*time.Second {
			t.Errorf("role %q: expected fallback to 180s, got %v", role, got)
		}
	}
}

func TestOllamaDiagnosticInfo(t *testing.T) {
	cfg := config.OllamaConfig{
		BaseURL:               "http://localhost:11434",
		DefaultModel:          "qwen2.5:7b-instruct",
		FastModel:             "llama3.2:3b",
		PlannerModel:          "qwen2.5:14b-instruct",
		TimeoutSeconds:        180,
		Timeout:               180 * time.Second,
		FastTimeoutSeconds:    90,
		FastTimeout:           90 * time.Second,
		PlannerTimeoutSeconds: 240,
		PlannerTimeout:        240 * time.Second,
	}
	p := NewOllamaProvider("ollama", cfg, zap.NewNop())

	info := p.DiagnosticInfo()
	if info["default_model"] != "qwen2.5:7b-instruct" {
		t.Errorf("unexpected default_model: %v", info["default_model"])
	}
	if info["fast_model"] != "llama3.2:3b" {
		t.Errorf("unexpected fast_model: %v", info["fast_model"])
	}
	if info["planner_model"] != "qwen2.5:14b-instruct" {
		t.Errorf("unexpected planner_model: %v", info["planner_model"])
	}
	// review_model not set, should show fallback
	expected := "qwen2.5:7b-instruct (fallback)"
	if info["review_model"] != expected {
		t.Errorf("expected review_model %q, got %v", expected, info["review_model"])
	}
}

func TestModelRoleIsValid(t *testing.T) {
	for _, role := range ValidModelRoles {
		if !role.IsValid() {
			t.Errorf("expected role %q to be valid", role)
		}
	}
	if ModelRole("unknown").IsValid() {
		t.Error("expected 'unknown' role to be invalid")
	}
}

func TestOllamaChatRequest_ThinkModeSerialization(t *testing.T) {
	marshal := func(v interface{}) map[string]interface{} {
		data, _ := json.Marshal(v)
		var m map[string]interface{}
		json.Unmarshal(data, &m)
		return m
	}

	req1 := ollamaChatRequest{Model: "test", Stream: false}
	m1 := marshal(req1)
	if _, has := m1["think"]; has {
		t.Error("expected 'think' to be absent when nil")
	}

	thinkTrue := true
	req2 := ollamaChatRequest{Model: "test", Stream: false, Think: &thinkTrue}
	m2 := marshal(req2)
	if m2["think"] != true {
		t.Errorf("expected think=true, got %v", m2["think"])
	}

	thinkFalse := false
	req3 := ollamaChatRequest{Model: "test", Stream: false, Think: &thinkFalse}
	m3 := marshal(req3)
	if m3["think"] != false {
		t.Errorf("expected think=false, got %v", m3["think"])
	}
}

func TestOllamaTokenExtraction(t *testing.T) {
	startFakeOllama := func(t *testing.T, body string, status int) string {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write([]byte(body)) //nolint:errcheck
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return srv.URL
	}

	t.Run("extracts prompt and completion tokens", func(t *testing.T) {
		payload := `{
			"model": "qwen2.5:7b-instruct",
			"message": {"role": "assistant", "content": "hello world"},
			"done": true,
			"prompt_eval_count": 42,
			"eval_count": 17
		}`
		url := startFakeOllama(t, payload, http.StatusOK)

		cfg := config.OllamaConfig{
			BaseURL:      url,
			DefaultModel: "qwen2.5:7b-instruct",
			Timeout:      5 * time.Second,
		}
		p := NewOllamaProvider("ollama", cfg, zap.NewNop())
		resp, err := p.Generate(context.Background(), GenerateRequest{UserPrompt: "hi"})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if resp.TokensPrompt != 42 {
			t.Errorf("TokensPrompt: want 42, got %d", resp.TokensPrompt)
		}
		if resp.TokensCompletion != 17 {
			t.Errorf("TokensCompletion: want 17, got %d", resp.TokensCompletion)
		}
		if resp.TokensTotal != 59 {
			t.Errorf("TokensTotal: want 59, got %d", resp.TokensTotal)
		}
	})

	t.Run("zero tokens when ollama omits counts", func(t *testing.T) {
		payload := `{
			"model": "qwen2.5:7b-instruct",
			"message": {"role": "assistant", "content": "hi"},
			"done": true
		}`
		url := startFakeOllama(t, payload, http.StatusOK)

		cfg := config.OllamaConfig{
			BaseURL:      url,
			DefaultModel: "qwen2.5:7b-instruct",
			Timeout:      5 * time.Second,
		}
		p := NewOllamaProvider("ollama", cfg, zap.NewNop())
		resp, err := p.Generate(context.Background(), GenerateRequest{UserPrompt: "hi"})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if resp.TokensTotal != 0 {
			t.Errorf("expected 0 tokens when omitted, got %d", resp.TokensTotal)
		}
	})
}
