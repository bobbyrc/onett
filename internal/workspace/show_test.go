package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// showCommitMain commits files to the default branch of the test's origin, as merged work would be.
func showCommitMain(t *testing.T, repo Repo, seed string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		writeFile(t, filepath.Join(seed, name), text)
		runGit(t, seed, "add", name)
	}
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "add files")
	runGit(t, seed, "push", repo.Remote, "HEAD:main")
}

func TestReadFileAtReadsTheCommittedText(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	showCommitMain(t, repo, seed, map[string]string{"AGENTS.md": "base rules\n"})
	require.NoError(t, m.EnsureClone(ctx, repo))
	ws, err := m.Prepare(ctx, repo, "feature", "run1")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(ws.Dir, "AGENTS.md"), []byte("edited\n"), 0o644))

	text, ok, err := m.ReadFileAt(ctx, repo, "origin/main", "AGENTS.md", 0)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "base rules", text)
}

func TestReadFileAtMissingFileIsNotAnError(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	showCommitMain(t, repo, seed, map[string]string{"docs/x.md": "doc\n"})
	require.NoError(t, m.EnsureClone(ctx, repo))

	text, ok, err := m.ReadFileAt(ctx, repo, "origin/main", "NOPE.md", 0)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", text)

	text, ok, err = m.ReadFileAt(ctx, repo, "origin/main", "docs", 0)
	require.NoError(t, err)
	assert.False(t, ok, "a directory is not a file")
	assert.Equal(t, "", text)
}

func TestReadFileAtUnknownRefIsAnError(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))

	_, _, err := m.ReadFileAt(ctx, repo, "origin/no-such-branch", "README.md", 0)
	assert.Error(t, err)
}

func TestReadFileAtCutsAtLimit(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	showCommitMain(t, repo, seed, map[string]string{"U.md": "héllo"})
	require.NoError(t, m.EnsureClone(ctx, repo))

	// The é is two bytes; a limit that lands inside it must back up to the rune's start.
	text, ok, err := m.ReadFileAt(ctx, repo, "origin/main", "U.md", 2)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "h", text)

	text, _, err = m.ReadFileAt(ctx, repo, "origin/main", "U.md", 3)
	require.NoError(t, err)
	assert.Equal(t, "hé", text)

	text, _, err = m.ReadFileAt(ctx, repo, "origin/main", "U.md", 0)
	require.NoError(t, err)
	assert.Equal(t, "héllo", text)
}
