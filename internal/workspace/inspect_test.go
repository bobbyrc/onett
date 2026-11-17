package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inspGit runs git in dir for test setup, failing the test on error. Its commit identity is passed
// on the command line, so no machine config is needed.
func inspGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// inspBareRepo makes a local bare repository whose HEAD names master, with one commit. The
// repository is a local path, so a test never reaches a real host.
func inspBareRepo(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	inspGit(t, root, "init", "-b", "master", seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644))
	inspGit(t, seed, "add", "-A")
	inspGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "init")
	bare := filepath.Join(root, name)
	inspGit(t, root, "clone", "--bare", "--quiet", seed, bare)
	return bare
}

// inspManager returns a Manager whose worktrees and scratch directories are in a fresh temp dir.
func inspManager(t *testing.T) *Manager {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "worktrees"))
}

func TestPrepareCopyClonesThenReuses(t *testing.T) {
	bare := inspBareRepo(t, "project.git")
	m := inspManager(t)
	r := Repo{Name: "project", Remote: bare, Path: filepath.Join(t.TempDir(), "repos", "project")}

	created, err := m.PrepareCopy(context.Background(), r)
	require.NoError(t, err)
	assert.True(t, created)
	assert.FileExists(t, filepath.Join(r.Path, ".git", "HEAD"))

	created, err = m.PrepareCopy(context.Background(), r)
	require.NoError(t, err)
	assert.False(t, created)
}

func TestPrepareCopyRefusesDirectoryWithoutGit(t *testing.T) {
	bare := inspBareRepo(t, "project.git")
	m := inspManager(t)
	path := filepath.Join(t.TempDir(), "project")
	require.NoError(t, os.MkdirAll(path, 0o755))

	_, err := m.PrepareCopy(context.Background(), Repo{Name: "project", Remote: bare, Path: path})
	assert.ErrorIs(t, err, ErrNotOnettCopy)
}

func TestPrepareCopyRefusesCloneOfAnotherRepository(t *testing.T) {
	bare := inspBareRepo(t, "project.git")
	other := inspBareRepo(t, "other.git")
	m := inspManager(t)
	path := filepath.Join(t.TempDir(), "project")

	created, err := m.PrepareCopy(context.Background(), Repo{Name: "project", Remote: other, Path: path})
	require.NoError(t, err)
	require.True(t, created)

	_, err = m.PrepareCopy(context.Background(), Repo{Name: "project", Remote: bare, Path: path})
	assert.ErrorIs(t, err, ErrNotOnettCopy)
	assert.NotContains(t, err.Error(), other, "the error must not carry the clone's URL")
}

func TestPrepareCopyRefusesUncommittedChange(t *testing.T) {
	bare := inspBareRepo(t, "project.git")
	m := inspManager(t)
	path := filepath.Join(t.TempDir(), "project")
	r := Repo{Name: "project", Remote: bare, Path: path}

	_, err := m.PrepareCopy(context.Background(), r)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(path, "README.md"), []byte("edited\n"), 0o644))

	_, err = m.PrepareCopy(context.Background(), r)
	assert.ErrorIs(t, err, ErrNotOnettCopy)
}

func TestRemoteDefaultBranchReadsMaster(t *testing.T) {
	bare := inspBareRepo(t, "project.git")
	m := inspManager(t)
	r := Repo{Name: "project", Remote: bare, Path: filepath.Join(t.TempDir(), "project")}
	_, err := m.PrepareCopy(context.Background(), r)
	require.NoError(t, err)

	branch, err := m.RemoteDefaultBranch(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "master", branch)
}
