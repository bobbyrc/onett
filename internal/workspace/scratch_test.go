package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScratchCredentialedGitDoesNotReadCloneConfig(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))

	prog, marker := plantProgram(t, "")
	// evil.git holds a different commit on main, so a fetch sent there would be visible.
	root := t.TempDir()
	work := filepath.Join(root, "work")
	runGit(t, root, "clone", repo.Remote, work)
	writeFile(t, filepath.Join(work, "evil.txt"), "evil\n")
	runGit(t, work, "add", "-A")
	runGit(t, work, "-c", "user.name=Evil", "-c", "user.email=evil@example.com", "commit", "-m", "evil")
	evil := filepath.Join(root, "evil.git")
	runGit(t, root, "init", "--bare", "-b", "main", evil)
	runGit(t, work, "push", evil, "main")

	s, err := newScratch(ctx, m.scratchDir(), repo.Path)
	require.NoError(t, err)
	defer s.close()

	// Planted straight into the file, so no helper of the package repairs it first.
	f, err := os.OpenFile(filepath.Join(repo.Path, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("[core]\n\tfsmonitor = " + prog + "\n\talternateRefsCommand = " + prog +
		"\n[url \"" + evil + "\"]\n\tinsteadOf = " + repo.Remote + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = s.gitNet(ctx, "fetch", "--quiet", repo.Remote, "+refs/heads/*:refs/onett/*")
	assertNotRun(t, marker, "credentialed fetch")
	require.NoError(t, err)
	assert.Equal(t, runGit(t, repo.Remote, "rev-parse", "refs/heads/main"),
		runGit(t, s.dir, "rev-parse", "refs/onett/main"))

	// Nothing repaired the clone's config in between, so the fetch really did not read it.
	data, err := os.ReadFile(filepath.Join(repo.Path, ".git", "config"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "insteadOf")
}

func TestScratchIsNotCreatedInTempDir(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	ws, err := m.Prepare(ctx, repo, "onett/s", "run1")
	require.NoError(t, err)
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "changed\n")
	committed, err := m.Commit(ctx, ws, "work")
	require.NoError(t, err)
	require.True(t, committed)
	require.NoError(t, m.Push(ctx, repo, ws))
	require.NoError(t, m.Fetch(ctx, repo))
	_, err = m.Diff(ctx, repo, "main", "onett/s")
	require.NoError(t, err)

	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "onett-git-"), "scratch left in temp dir: %s", e.Name())
	}
	left, err := os.ReadDir(m.scratchDir())
	require.NoError(t, err)
	assert.Empty(t, left, "every scratch repository is deleted after use")
}

func TestRepairConfigDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secrets.env")
	const secretText = "[user]\n\tname = TOP-SECRET-VALUE\n"
	require.NoError(t, os.WriteFile(secret, []byte(secretText), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(dir, "config")))

	require.NoError(t, repairConfig(dir, ""))

	fi, err := os.Lstat(filepath.Join(dir, "config"))
	require.NoError(t, err)
	assert.True(t, fi.Mode().IsRegular())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		assert.NotContains(t, string(data), "TOP-SECRET-VALUE", e.Name())
	}
	got, err := os.ReadFile(secret)
	require.NoError(t, err)
	assert.Equal(t, secretText, string(got))
}

func TestRepairConfigDoesNotBlockOnFIFO(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "config"), 0o644))

	done := make(chan error, 1)
	go func() { done <- repairConfig(dir, "") }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("repairConfig blocked on a FIFO named config")
	}

	fi, err := os.Lstat(filepath.Join(dir, "config"))
	require.NoError(t, err)
	assert.True(t, fi.Mode().IsRegular())
	data, err := os.ReadFile(filepath.Join(dir, "config"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "bare = false")
}

func TestRepairConfigIgnoresHugeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("[core]\n", 2<<20/len("[core]\n"))), 0o644))

	require.NoError(t, repairConfig(dir, ""))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "[core]\n\tbare = false\n", string(got))
	assert.NoFileExists(t, filepath.Join(dir, "config.pre-onett"))
}
