package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateLoader_LoadNonExistent(t *testing.T) {
	l := NewTemplateLoader("/nonexistent/path")
	_, err := l.Load("no_tpl", "v1")
	if err == nil {
		t.Error("expected error loading non-existent template, got nil")
	}
}

func TestTemplateLoader_Render(t *testing.T) {
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "greet")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `id: greet
version: v1
purpose: greeting
system_prompt: "You are helpful."
user_prompt_tpl: "Hello, {{index . \"name\"}}!"
output_schema: {}
`
	if err := os.WriteFile(filepath.Join(tplDir, "v1.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	l := NewTemplateLoader(dir)
	tpl, err := l.Load("greet", "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rendered, err := l.Render(tpl, map[string]string{"name": "World"})
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	if rendered != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", rendered)
	}
}
