package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunShimIsOffByDefaultAndCanBeEnabled(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)
	assert.False(t, cfg.Limits.RunShim)

	cfg, err = Parse([]byte("limits:\n  run_shim: true\n"), "")
	require.NoError(t, err)
	assert.True(t, cfg.Limits.RunShim)
}
