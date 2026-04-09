package profile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DSL is permanently disabled. These tests verify that calling DSL functions
// returns an explicit error instead of panicking or silently succeeding.

func TestParseProfile_Disabled(t *testing.T) {
	_, err := ParseProfile("qwen2.5:7b-instruct?think=on&timeout=240")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSL profile parsing is disabled")
}

func TestParseProfile_Empty_Disabled(t *testing.T) {
	_, err := ParseProfile("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSL profile parsing is disabled")
}

func TestParseProfileOrSingle_PlainModel_Disabled(t *testing.T) {
	_, err := ParseProfileOrSingle("llama3.2:3b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSL profile parsing is disabled")
}

func TestParseProfileOrSingle_Empty_Disabled(t *testing.T) {
	_, err := ParseProfileOrSingle("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSL profile parsing is disabled")
}

func TestParseProfileOrSingle_DSL_Disabled(t *testing.T) {
	_, err := ParseProfileOrSingle("qwen2.5:7b-instruct?think=on|llama3.2:3b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSL profile parsing is disabled")
}
