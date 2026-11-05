package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/engine"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store/sqlite"
	"github.com/bobbyrc/onett/internal/workspace"
)

func TestWriteStarterConfigCreatesOnceAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf", "onett.yaml")

	created, err := writeStarterConfig(path)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, initialConfig("", "", ""), readFile(t, path), "serve and init share one generator")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	cfg, err := loadConfig(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Projects)

	require.NoError(t, os.WriteFile(path, []byte("server:\n  addr: 127.0.0.1:9\n"), 0o600))
	created, err = writeStarterConfig(path)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, "server:\n  addr: 127.0.0.1:9\n", readFile(t, path), "an existing file is left alone")
}

func TestBrokenConfigStillFailsToLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server: [not, a, map\n"), 0o600))

	created, err := writeStarterConfig(path)
	require.NoError(t, err)
	assert.False(t, created)
	_, err = loadConfig(path)
	assert.Error(t, err)
	assert.Equal(t, "server: [not, a, map\n", readFile(t, path))
}

// The starter config has no projects. The engine must start, idle and stop without errors.
func TestEngineRunsCleanlyWithStarterConfigAndNoProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	_, err := writeStarterConfig(path)
	require.NoError(t, err)
	cfg, err := loadConfig(path)
	require.NoError(t, err)
	cfg.Server.DataDir = t.TempDir()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	eng, err := engine.New(engine.Deps{
		Config:        cfg,
		Store:         st,
		Bus:           events.NewBus(st, logger),
		Workspaces:    workspace.New(filepath.Join(cfg.Server.DataDir, "worktrees")),
		Logger:        logger,
		SCMFor:        scmFor,
		HarnessFor:    harnessFor,
		SchedulerTick: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not stop")
	}
	assert.Empty(t, logs.String(), "no warnings or errors while idle")
	assert.Empty(t, eng.Config().Projects)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
