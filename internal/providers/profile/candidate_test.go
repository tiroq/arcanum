package profile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThinkMode_IsValid(t *testing.T) {
	tests := []struct {
		mode ThinkMode
		want bool
	}{
		{ThinkDefault, true},
		{ThinkEnabled, true},
		{ThinkDisabled, true},
		{ThinkMode("invalid"), false},
		{ThinkMode("THINKING"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.mode.IsValid())
		})
	}
}

func TestThinkMode_String(t *testing.T) {
	assert.Equal(t, "default", ThinkDefault.String())
	assert.Equal(t, "thinking", ThinkEnabled.String())
	assert.Equal(t, "nothinking", ThinkDisabled.String())
}

func TestParseThinkMode(t *testing.T) {
	tests := []struct {
		input   string
		want    ThinkMode
		wantErr bool
	}{
		{"", ThinkDefault, false},
		{"default", ThinkDefault, false},
		{"thinking", ThinkEnabled, false},
		{"think", ThinkEnabled, false},
		{"nothinking", ThinkDisabled, false},
		{"nothink", ThinkDisabled, false},
		{"on", ThinkEnabled, false},
		{"off", ThinkDisabled, false},
		{"invalid", ThinkDefault, true},
		{"THINKING", ThinkDefault, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseThinkMode(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid think mode")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestModelCandidate_Validate(t *testing.T) {
	tests := []struct {
		name    string
		c       ModelCandidate
		wantErr string
	}{
		{
			name: "valid with defaults",
			c:    ModelCandidate{ModelName: "llama3.2:3b"},
		},
		{
			name: "valid with all fields",
			c: ModelCandidate{
				ModelName: "qwen2.5:7b-instruct",
				ThinkMode: ThinkEnabled,
				Timeout:   120 * time.Second,
				JSONMode:  true,
			},
		},
		{
			name:    "empty model name",
			c:       ModelCandidate{},
			wantErr: "model name is required",
		},
		{
			name: "invalid think mode",
			c: ModelCandidate{
				ModelName: "llama3.2:3b",
				ThinkMode: ThinkMode("bogus"),
			},
			wantErr: "invalid think mode",
		},
		{
			name: "negative timeout",
			c: ModelCandidate{
				ModelName: "llama3.2:3b",
				Timeout:   -1 * time.Second,
			},
			wantErr: "timeout must be non-negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.c.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
