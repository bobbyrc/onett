package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/policy"
)

func TestDiffRatesRenamedProtectedFileUnderItsOldPath(t *testing.T) {
	repo, m, seed := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()

	writeFile(t, filepath.Join(seed, "internal", "engine", "loop_shepherd.go"), "package engine\n"+numberedLines(19))
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "shepherd")
	runGit(t, seed, "push", repo.Remote, "main")

	ws, err := m.Prepare(ctx, repo, "onett/mv", "run1")
	require.NoError(t, err)
	runGit(t, ws.Dir, "mv", "internal/engine/loop_shepherd.go", "internal/engine/loop_gate.go")
	_, err = m.Commit(ctx, ws, "move shepherd")
	require.NoError(t, err)
	require.NoError(t, m.Push(ctx, repo, ws))

	changes, err := m.Diff(ctx, repo, "main", "onett/mv")
	require.NoError(t, err)
	assert.ElementsMatch(t, []policy.FileChange{
		{Path: "internal/engine/loop_shepherd.go", Added: 0, Deleted: 20},
		{Path: "internal/engine/loop_gate.go", Added: 20, Deleted: 0},
	}, changes)

	d := policy.Evaluate(config.DefaultPolicy(), policy.Input{Files: changes})
	assert.Equal(t, domain.RiskHigh, d.Risk)
}

func TestDiffReportsUnusualPathsExactly(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()

	ws, err := m.Prepare(ctx, repo, "onett/odd", "run1")
	require.NoError(t, err)
	writeFile(t, filepath.Join(ws.Dir, "auth", "café.go"), "x\n")
	writeFile(t, filepath.Join(ws.Dir, "auth", "a => b.go"), "x\n")
	_, err = m.Commit(ctx, ws, "odd names")
	require.NoError(t, err)
	require.NoError(t, m.Push(ctx, repo, ws))

	changes, err := m.Diff(ctx, repo, "main", "onett/odd")
	require.NoError(t, err)
	paths := make([]string, 0, len(changes))
	for _, c := range changes {
		paths = append(paths, c.Path)
	}
	assert.ElementsMatch(t, []string{"auth/café.go", "auth/a => b.go"}, paths)
}
