package main

import (
	"bytes"
	"testing"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunShimIsDispatchedButNotListed(t *testing.T) {
	var out, errOut bytes.Buffer
	assert.Equal(t, 2, run([]string{"run-shim"}, &out, &errOut), "missing --spec is a usage error")
	assert.Contains(t, errOut.String(), "--spec is required")
	assert.NotContains(t, errOut.String(), "unknown command")

	out.Reset()
	assert.Equal(t, 0, run([]string{"help"}, &out, &errOut))
	assert.NotContains(t, out.String(), "run-shim", "the daemon starts it; people never do")
}

func TestHarnessWithShimBuildsEitherWay(t *testing.T) {
	h := config.Harness{Type: "exec", Command: []string{"true"}}
	for _, on := range []bool{false, true} {
		cfg := &config.Config{}
		cfg.Limits.RunShim = &on
		got, err := harnessWithShim("plug", h, cfg, "/path/to/onett")
		require.NoError(t, err)
		assert.NotNil(t, got)
	}
	_, err := harnessWithShim("plug", h, nil, "/path/to/onett")
	require.NoError(t, err, "a nil config means no shim")
}
