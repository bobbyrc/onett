package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHarnessIsolateIsOnByDefaultAndCanBeDisabled(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)
	assert.True(t, cfg.Harness["claude"].IsolateOn())

	cfg, err = Parse([]byte("harnesses:\n  claude:\n    type: claude-code\n    isolate: false\n"), "")
	require.NoError(t, err)
	assert.False(t, cfg.Harness["claude"].IsolateOn())
}
