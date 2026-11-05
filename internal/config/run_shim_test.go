package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunShimIsOnByDefaultAndCanBeDisabled(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)
	assert.True(t, cfg.Limits.RunShimOn())

	cfg, err = Parse([]byte("limits:\n  run_shim: false\n"), "")
	require.NoError(t, err)
	assert.False(t, cfg.Limits.RunShimOn())

	cfg, err = Parse([]byte("limits:\n  run_shim: true\n"), "")
	require.NoError(t, err)
	assert.True(t, cfg.Limits.RunShimOn())
}
