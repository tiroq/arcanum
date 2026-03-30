package prompts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Template holds a loaded prompt template.
type Template struct {
	ID            string          `yaml:"id"`
	Version       string          `yaml:"version"`
	Purpose       string          `yaml:"purpose"`
	SystemPrompt  string          `yaml:"system_prompt"`
	UserPromptTpl string          `yaml:"user_prompt_tpl"`
	OutputSchema  json.RawMessage `yaml:"-"`
}

// yamlTemplate is an intermediate struct for YAML parsing.
type yamlTemplate struct {
	ID            string      `yaml:"id"`
	Version       string      `yaml:"version"`
	Purpose       string      `yaml:"purpose"`
	SystemPrompt  string      `yaml:"system_prompt"`
	UserPromptTpl string      `yaml:"user_prompt_tpl"`
	OutputSchema  interface{} `yaml:"output_schema"`
}

// TemplateLoader loads templates from the filesystem with an in-memory cache.
type TemplateLoader struct {
	basePath string
	cache    map[string]*Template
	mu       sync.RWMutex
}

// NewTemplateLoader creates a TemplateLoader rooted at basePath.
func NewTemplateLoader(basePath string) *TemplateLoader {
	return &TemplateLoader{
		basePath: basePath,
		cache:    make(map[string]*Template),
	}
}

// Load reads a template from {basePath}/{templateID}/{version}.yaml, caching it.
func (l *TemplateLoader) Load(templateID, version string) (*Template, error) {
	key := templateID + "/" + version

	l.mu.RLock()
	if tpl, ok := l.cache[key]; ok {
		l.mu.RUnlock()
		return tpl, nil
	}
	l.mu.RUnlock()

	path := filepath.Join(l.basePath, templateID, version+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load template %s: %w", key, err)
	}

	var raw yamlTemplate
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse template %s: %w", key, err)
	}

	schemaJSON, err := json.Marshal(raw.OutputSchema)
	if err != nil {
		schemaJSON = json.RawMessage("{}")
	}

	tpl := Template{
		ID:            raw.ID,
		Version:       raw.Version,
		Purpose:       raw.Purpose,
		SystemPrompt:  raw.SystemPrompt,
		UserPromptTpl: raw.UserPromptTpl,
		OutputSchema:  schemaJSON,
	}

	l.mu.Lock()
	l.cache[key] = &tpl
	l.mu.Unlock()

	return &tpl, nil
}

// Render executes the UserPromptTpl with the provided variables.
func (l *TemplateLoader) Render(tpl *Template, vars map[string]string) (string, error) {
	t, err := template.New("prompt").Parse(tpl.UserPromptTpl)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("render prompt template: %w", err)
	}
	return buf.String(), nil
}
