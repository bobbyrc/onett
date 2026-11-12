package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasUncommittedWork(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/u", "run1")
	require.NoError(t, err)
	has := func() bool {
		t.Helper()
		got, err := m.HasUncommittedWork(ctx, ws)
		require.NoError(t, err)
		return got
	}

	assert.False(t, has(), "a fresh worktree")
	writeFile(t, filepath.Join(ws.Dir, ".onett-run", "result.json"), "{}")
	assert.False(t, has(), "the outbox is not work")
	writeFile(t, filepath.Join(ws.Dir, "new.txt"), "untracked\n")
	assert.True(t, has(), "an untracked file")
	_, err = m.Commit(ctx, ws, "save")
	require.NoError(t, err)
	assert.False(t, has(), "after a commit")
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "changed\n")
	assert.True(t, has(), "a modified tracked file")

	// A worktree whose .git was replaced is an error, not an answer.
	require.NoError(t, os.Rename(filepath.Join(ws.Dir, ".git"), filepath.Join(ws.Dir, ".git.orig")))
	runGit(t, ws.Dir, "init")
	_, err = m.HasUncommittedWork(ctx, ws)
	require.ErrorIs(t, err, errNotCheckout)
}

// A checkout a person made with `git init` has no origin remote. Onett takes the remote from the
// project config, so it works all the same.
func TestEnsureCloneWorksOnCheckoutWithoutOrigin(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	runGit(t, filepath.Dir(repo.Path), "init", "-b", "main", repo.Path)

	require.NoError(t, m.EnsureClone(ctx, repo))

	assert.Equal(t, runGit(t, repo.Remote, "rev-parse", "refs/heads/main"), runGit(t, repo.Path, "rev-parse", "HEAD"))
	assert.Equal(t, repo.Remote, runGit(t, repo.Path, "config", "remote.origin.url"))
}
