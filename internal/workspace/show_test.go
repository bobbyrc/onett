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

	text, ok, err := m.ReadFileAt(ctx, repo, "refs/remotes/origin/main", "AGENTS.md")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "base rules", text)
}

func TestReadFileAtMissingFileIsNotAnError(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	showCommitMain(t, repo, seed, map[string]string{"docs/x.md": "doc\n"})
	require.NoError(t, m.EnsureClone(ctx, repo))

	text, ok, err := m.ReadFileAt(ctx, repo, "refs/remotes/origin/main", "NOPE.md")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", text)

	text, ok, err = m.ReadFileAt(ctx, repo, "refs/remotes/origin/main", "docs")
	require.NoError(t, err)
	assert.False(t, ok, "a directory is not a file")
	assert.Equal(t, "", text)
}

func TestReadFileAtUnknownRefIsAnError(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))

	_, _, err := m.ReadFileAt(ctx, repo, "refs/remotes/origin/no-such-branch", "README.md")
	assert.Error(t, err)
}

func TestReadFileAtIgnoresReplaceRefs(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	showCommitMain(t, repo, seed, map[string]string{"AGENTS.md": "base rules\n"})
	require.NoError(t, m.EnsureClone(ctx, repo))

	blob := runGit(t, repo.Path, "rev-parse", "refs/remotes/origin/main:AGENTS.md")
	swap := filepath.Join(t.TempDir(), "swap.md")
	require.NoError(t, os.WriteFile(swap, []byte("BRANCH RULE\n"), 0o644))
	other := runGit(t, repo.Path, "hash-object", "-w", swap)
	runGit(t, repo.Path, "replace", blob, other)

	text, ok, err := m.ReadFileAt(ctx, repo, "refs/remotes/origin/main", "AGENTS.md")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "base rules", text)
}

func TestReadFileAtDoesNotFollowASymlink(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	showCommitMain(t, repo, seed, map[string]string{"AGENTS.md": "base rules\n"})
	require.NoError(t, os.Symlink("AGENTS.md", filepath.Join(seed, "CLAUDE.md")))
	runGit(t, seed, "add", "CLAUDE.md")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "add link")
	runGit(t, seed, "push", repo.Remote, "HEAD:main")
	require.NoError(t, m.EnsureClone(ctx, repo))

	text, ok, err := m.ReadFileAt(ctx, repo, "refs/remotes/origin/main", "CLAUDE.md")
	require.NoError(t, err)
	assert.False(t, ok, "a symbolic link is reported as missing, not followed")
	assert.Equal(t, "", text)

	text, ok, err = m.ReadFileAt(ctx, repo, "refs/remotes/origin/main", "AGENTS.md")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "base rules", text)
}

func TestReadFileAtShortRefCanBeShadowedButFullRefCannot(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	showCommitMain(t, repo, seed, map[string]string{"AGENTS.md": "base rules\n"})
	runGit(t, seed, "checkout", "-b", "evil")
	writeFile(t, filepath.Join(seed, "AGENTS.md"), "BRANCH RULE\n")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-am", "evil rules")
	runGit(t, seed, "push", repo.Remote, "evil")
	runGit(t, seed, "checkout", "-")
	require.NoError(t, m.EnsureClone(ctx, repo))
	runGit(t, repo.Path, "tag", "origin/main", "refs/remotes/origin/evil")

	text, ok, err := m.ReadFileAt(ctx, repo, "refs/remotes/origin/main", "AGENTS.md")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "base rules", text)
}
