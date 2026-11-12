package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bobbyrc/onett/internal/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tamperedDiffSetup makes a run that changes two files, pushes it, and checks the honest diff.
// The agent-writable clone is then tampered with by each test.
func tamperedDiffSetup(t *testing.T) (Repo, *Manager, Workspace, context.Context) {
	t.Helper()
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/t", "run1")
	require.NoError(t, err)
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "hello\nworld\n")
	writeFile(t, filepath.Join(ws.Dir, "internal", "auth", "gate.go"), "package auth\n")
	committed, err := m.Commit(ctx, ws, "work")
	require.NoError(t, err)
	require.True(t, committed)
	require.NoError(t, m.Push(ctx, repo, ws))

	got, err := m.Diff(ctx, repo, "main", "onett/t")
	require.NoError(t, err)
	assert.ElementsMatch(t, wantDiff, got)
	return repo, m, ws, ctx
}

// wantDiff is the honest diff of tamperedDiffSetup.
var wantDiff = []policy.FileChange{
	{Path: "README.md", Added: 1, Deleted: 0},
	{Path: "internal/auth/gate.go", Added: 1, Deleted: 0},
}

func TestDiffIgnoresReplaceRefsInClone(t *testing.T) {
	repo, m, _, ctx := tamperedDiffSetup(t)
	tree := runGit(t, repo.Path, "rev-parse", "refs/heads/onett/t^{tree}")
	base := runGit(t, repo.Path, "rev-parse", "refs/remotes/origin/main^{tree}")
	runGit(t, repo.Path, "replace", tree, base)

	got, err := m.Diff(ctx, repo, "main", "onett/t")
	require.NoError(t, err)
	assert.ElementsMatch(t, wantDiff, got)
}

func TestDiffIgnoresGraftsInClone(t *testing.T) {
	repo, m, _, ctx := tamperedDiffSetup(t)
	head := runGit(t, repo.Path, "rev-parse", "refs/heads/onett/t")
	mainSHA := runGit(t, repo.Path, "rev-parse", "refs/remotes/origin/main")
	writeFile(t, filepath.Join(repo.Path, ".git", "info", "grafts"), mainSHA+" "+head+"\n")

	got, err := m.Diff(ctx, repo, "main", "onett/t")
	require.NoError(t, err)
	assert.ElementsMatch(t, wantDiff, got)
}

func TestDiffIgnoresAttributesInClone(t *testing.T) {
	repo, m, _, ctx := tamperedDiffSetup(t)
	writeFile(t, filepath.Join(repo.Path, ".git", "info", "attributes"), "* -diff\n")
	writeFile(t, filepath.Join(repo.Path, ".gitattributes"), "* -diff\n")

	got, err := m.Diff(ctx, repo, "main", "onett/t")
	require.NoError(t, err)
	assert.ElementsMatch(t, wantDiff, got)
}
