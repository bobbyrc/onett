package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPolicyAutoMergesEveryRiskLevel(t *testing.T) {
	for level, g := range DefaultGates() {
		assert.True(t, g.AutoMerge, level)
		assert.True(t, g.RequireReview, level)
		assert.True(t, g.RequireCI, level)
	}
}

func TestSupervisedPolicyIsBuiltIn(t *testing.T) {
	cfg, err := Parse([]byte("projects:\n  demo:\n    repo: o/r\n    policy: supervised\n"), "")
	require.NoError(t, err)

	sup, ok := cfg.Policies["supervised"]
	require.True(t, ok, "supervised is added without the user defining it")
	assert.Equal(t, SupervisedGates(), sup.Gates)
	assert.True(t, sup.Gates["low"].AutoMerge)
	assert.False(t, sup.Gates["medium"].AutoMerge)
	assert.False(t, sup.Gates["high"].AutoMerge)
	assert.True(t, sup.Gates["high"].RequireReview)
	assert.True(t, sup.Gates["high"].RequireCI)
	assert.Equal(t, cfg.Policies["default"].Rules, sup.Rules)
	assert.Equal(t, cfg.Policies["default"].MaxLines, sup.MaxLines)
	assert.Equal(t, cfg.Policies["default"].CIGrace, sup.CIGrace)
	assert.Equal(t, "supervised", cfg.Projects["demo"].Policy)
}

func TestUserDefinedSupervisedOverridesBuiltIn(t *testing.T) {
	cfg, err := Parse([]byte(`
policies:
  supervised:
    max_lines: 10
    gates:
      high: {auto_merge: true, require_review: true, require_ci: true}
`), "")
	require.NoError(t, err)
	p := cfg.Policies["supervised"]
	assert.Equal(t, 10, p.MaxLines)
	assert.True(t, p.Gates["high"].AutoMerge)
}
