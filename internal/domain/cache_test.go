package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunCacheHitRate(t *testing.T) {
	rate, ok := Run{InputTokens: 1000, CachedInputTokens: 900}.CacheHitRate()
	assert.True(t, ok)
	assert.InDelta(t, 0.9, rate, 1e-9)

	_, ok = Run{InputTokens: 1000}.CacheHitRate()
	assert.False(t, ok, "no cache reads reported")
	_, ok = Run{}.CacheHitRate()
	assert.False(t, ok, "no input")
}
