package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveLayoutOfCloneAndWorktree(t *testing.T) {
	repo, m, _ := setup(t)
	ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
	require.NoError(t, err)

	cloneGit := filepath.Join(repo.Path, ".git")
	cl, err := resolveLayout(repo.Path)
	require.NoError(t, err)
	assert.Equal(t, cloneGit, cl.gitDir)
	assert.Equal(t, cloneGit, cl.commonDir)

	wl, err := resolveLayout(ws.Dir)
	require.NoError(t, err)
	wantCommon, err := filepath.EvalSymlinks(cloneGit)
	require.NoError(t, err)
	gotCommon, err := filepath.EvalSymlinks(wl.commonDir)
	require.NoError(t, err)
	assert.Equal(t, wantCommon, gotCommon)
	wantGit, err := filepath.EvalSymlinks(runGit(t, ws.Dir, "rev-parse", "--absolute-git-dir"))
	require.NoError(t, err)
	gotGit, err := filepath.EvalSymlinks(wl.gitDir)
	require.NoError(t, err)
	assert.Equal(t, wantGit, gotGit)

	require.NoError(t, requireWorktree(ws.Dir))
	assert.ErrorIs(t, requireWorktree(repo.Path), errNotCheckout)
	require.NoError(t, checkWorktreeOf(repo.Path, ws.Dir))
}

func TestResolveLayoutRefusesChangedGitFile(t *testing.T) {
	t.Run("missing .git", func(t *testing.T) {
		repo, m, _ := setup(t)
		ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
		require.NoError(t, err)
		dot := filepath.Join(ws.Dir, ".git")
		require.NoError(t, os.Rename(dot, dot+".gone"))
		_, err = resolveLayout(ws.Dir)
		assert.ErrorIs(t, err, errNotCheckout)
	})

	t.Run(".git replaced by a directory", func(t *testing.T) {
		repo, m, _ := setup(t)
		ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
		require.NoError(t, err)
		dot := filepath.Join(ws.Dir, ".git")
		require.NoError(t, os.Remove(dot))
		runGit(t, ws.Dir, "init", "-q")
		_, err = resolveLayout(ws.Dir)
		require.NoError(t, err)
		assert.ErrorIs(t, requireWorktree(ws.Dir), errNotCheckout)
	})

	t.Run(".git is a symlink", func(t *testing.T) {
		repo, m, _ := setup(t)
		ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
		require.NoError(t, err)
		dot := filepath.Join(ws.Dir, ".git")
		require.NoError(t, os.Rename(dot, dot+".real"))
		require.NoError(t, os.Symlink(dot+".real", dot))
		_, err = resolveLayout(ws.Dir)
		assert.ErrorIs(t, err, errNotCheckout)
	})

	t.Run(".git names a plain repository", func(t *testing.T) {
		repo, m, _ := setup(t)
		ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
		require.NoError(t, err)
		dot := filepath.Join(ws.Dir, ".git")
		require.NoError(t, os.WriteFile(dot, []byte("gitdir: "+filepath.Join(repo.Path, ".git")+"\n"), 0o644))
		_, err = resolveLayout(ws.Dir)
		assert.ErrorIs(t, err, errNotCheckout)
	})

	t.Run("worktree directory points somewhere else", func(t *testing.T) {
		repo, m, _ := setup(t)
		ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
		require.NoError(t, err)
		dot := filepath.Join(ws.Dir, ".git")
		fake := filepath.Join(t.TempDir(), "fake", "worktrees", "w")
		require.NoError(t, os.MkdirAll(fake, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(fake, "gitdir"),
			[]byte(filepath.Join(repo.Path, ".git")+"\n"), 0o644))
		require.NoError(t, os.WriteFile(dot, []byte("gitdir: "+fake+"\n"), 0o644))
		_, err = resolveLayout(ws.Dir)
		assert.ErrorIs(t, err, errNotCheckout)
	})

	t.Run("no gitdir prefix", func(t *testing.T) {
		repo, m, _ := setup(t)
		ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
		require.NoError(t, err)
		dot := filepath.Join(ws.Dir, ".git")
		require.NoError(t, os.WriteFile(dot, []byte("not a pointer\n"), 0o644))
		_, err = resolveLayout(ws.Dir)
		assert.ErrorIs(t, err, errNotCheckout)
	})
}

func TestCheckWorktreeOfRefusesAnotherRepository(t *testing.T) {
	repo, m, _ := setup(t)
	ws, err := m.Prepare(context.Background(), repo, "onett/lay", "run1")
	require.NoError(t, err)

	other := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(other), "clone", repo.Remote, other)
	decoy := filepath.Join(t.TempDir(), "decoy")
	runGit(t, other, "worktree", "add", "--detach", decoy, "origin/main")
	decoyGit := filepath.Join(other, ".git", "worktrees", "decoy")

	dot := filepath.Join(ws.Dir, ".git")
	require.NoError(t, os.WriteFile(dot, []byte("gitdir: "+decoyGit+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(decoyGit, "gitdir"), []byte(dot+"\n"), 0o644))

	require.NoError(t, requireWorktree(ws.Dir))
	assert.ErrorIs(t, checkWorktreeOf(repo.Path, ws.Dir), errNotCheckout)
}

func TestLocalEnvCarriesNoCredentials(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret-x")
	t.Setenv("ANTHROPIC_API_KEY", "secret-y")
	home := t.TempDir()
	t.Setenv("HOME", home)

	env := localEnv()
	assert.Contains(t, env, "HOME="+home)
	assert.Contains(t, env, "GIT_TERMINAL_PROMPT=0")
	for _, kv := range env {
		assert.NotContains(t, kv, "secret-x")
		assert.NotContains(t, kv, "secret-y")
	}
}
