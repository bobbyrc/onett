package main

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteStarterConfigUsesGivenAddr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	created, err := writeStarterConfig(path, "127.0.0.1:0")
	require.NoError(t, err)
	assert.True(t, created)
	cfg, err := loadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:0", cfg.Server.Addr)
}

func TestServeRejectsMalformedAddr(t *testing.T) {
	e := &env{out: io.Discard, errOut: io.Discard}
	err := cmdServe(e, []string{"--config", filepath.Join(t.TempDir(), "onett.yaml"), "--addr", "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--addr")
}
