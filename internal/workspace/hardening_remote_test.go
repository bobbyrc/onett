package workspace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommitRefusesWorktreeWhoseGitFileWasReplaced(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/dotgit", "run1")
	require.NoError(t, err)
	prog, marker := plantProgram(t, "")

	require.NoError(t, os.Rename(filepath.Join(ws.Dir, ".git"), filepath.Join(ws.Dir, ".git.orig")))
	runGit(t, ws.Dir, "init")
	runGit(t, ws.Dir, "config", "core.fsmonitor", prog)
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "changed\n")

	_, err = m.Commit(ctx, ws, "x")
	assertNotRun(t, marker, "commit")
	require.Error(t, err)
}

func TestPushRefusesWorktreeLinkedToAnotherRepository(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/link", "run1")
	require.NoError(t, err)
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "changed\n")
	_, err = m.Commit(ctx, ws, "work")
	require.NoError(t, err)

	// A second repository with its own worktree, whose back-pointer we make match the run's worktree.
	other := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(other), "clone", repo.Remote, other)
	runGit(t, other, "branch", "onett/link", "origin/main")
	decoy := filepath.Join(filepath.Dir(other), "decoy")
	runGit(t, other, "worktree", "add", "--detach", decoy, "origin/main")
	admin := filepath.Join(other, ".git", "worktrees", "decoy")

	writeFile(t, filepath.Join(ws.Dir, ".git"), "gitdir: "+admin+"\n")
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(ws.Dir, ".git")+"\n")

	err = m.Push(ctx, repo, ws)
	require.Error(t, err)
	_, err = gitExitCodeForTest(repo.Remote, "rev-parse", "--verify", "--quiet", "refs/heads/onett/link")
	require.Error(t, err, "origin must not receive the branch")
}

func TestPushIgnoresRedirectsInCloneConfig(t *testing.T) {
	cases := []struct {
		name string
		// key returns the config key to plant; value returns its value, given the evil and real remotes.
		key   func(evil string) string
		value func(evil, remote string) string
	}{
		{name: "remote.origin.pushurl",
			key:   func(string) string { return "remote.origin.pushurl" },
			value: func(evil, _ string) string { return evil }},
		{name: "url insteadOf",
			key:   func(evil string) string { return "url." + evil + ".insteadOf" },
			value: func(_, remote string) string { return remote }},
		{name: "url pushInsteadOf",
			key:   func(evil string) string { return "url." + evil + ".pushInsteadOf" },
			value: func(_, remote string) string { return remote }},
		{name: "remote.origin.url",
			key:   func(string) string { return "remote.origin.url" },
			value: func(evil, _ string) string { return evil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, m, _ := setup(t)
			t.Setenv("GH_TOKEN", canaryToken)
			ctx := context.Background()
			ws, err := m.Prepare(ctx, repo, "onett/r", "run1")
			require.NoError(t, err)

			evil := filepath.Join(t.TempDir(), "evil.git")
			runGit(t, filepath.Dir(evil), "init", "--bare", evil)
			runGit(t, ws.Dir, "config", c.key(evil), c.value(evil, repo.Remote))

			writeFile(t, filepath.Join(ws.Dir, "README.md"), "changed\n")
			_, err = m.Commit(ctx, ws, "work")
			require.NoError(t, err)
			require.NoError(t, m.Push(ctx, repo, ws))

			assert.Equal(t, "", runGit(t, evil, "for-each-ref"))
			sha, err := m.HeadSHA(ctx, ws)
			require.NoError(t, err)
			assert.Equal(t, sha, runGit(t, repo.Remote, "rev-parse", "refs/heads/onett/r"))
		})
	}
}

func TestFetchIgnoresRedirectedRemoteURL(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))

	// A bare repository whose main branch holds a different commit than origin's.
	evilWork := filepath.Join(t.TempDir(), "evil-work")
	runGit(t, filepath.Dir(evilWork), "clone", repo.Remote, evilWork)
	writeFile(t, filepath.Join(evilWork, "evil.txt"), "evil\n")
	runGit(t, evilWork, "add", "-A")
	runGit(t, evilWork, "-c", "user.name=Evil", "-c", "user.email=evil@example.com", "commit", "-m", "evil")
	evil := filepath.Join(t.TempDir(), "evil.git")
	runGit(t, filepath.Dir(evil), "init", "--bare", evil)
	runGit(t, evilWork, "push", evil, "main")

	runGit(t, repo.Path, "config", "remote.origin.url", evil)
	require.NoError(t, m.Fetch(ctx, repo))

	assert.Equal(t, runGit(t, repo.Remote, "rev-parse", "refs/heads/main"),
		runGit(t, repo.Path, "rev-parse", "refs/remotes/origin/main"))
	assert.Equal(t, repo.Remote, runGit(t, repo.Path, "config", "remote.origin.url"))
}

// unauthorizedServer answers every request with 401 and a Basic challenge, so git asks for
// credentials. It listens on 127.0.0.1 only.
func unauthorizedServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git"
}

func TestCredentialProgramsFromCloneConfigNeverRun(t *testing.T) {
	cases := []struct {
		name string
		url  func(t *testing.T) string
		key  string
		ssh  bool
	}{
		{name: "credential.helper", url: unauthorizedServer, key: "credential.helper"},
		{name: "core.askPass", url: unauthorizedServer, key: "core.askPass"},
		{name: "core.sshCommand", url: func(*testing.T) string { return "ssh://git@127.0.0.1/repo.git" },
			key: "core.sshCommand", ssh: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, m, _ := setup(t)
			t.Setenv("GH_TOKEN", canaryToken)
			ctx := context.Background()
			ws, err := m.Prepare(ctx, repo, "onett/c", "run1")
			require.NoError(t, err)
			writeFile(t, filepath.Join(ws.Dir, "README.md"), "changed\n")
			_, err = m.Commit(ctx, ws, "work")
			require.NoError(t, err)

			prog, marker := plantProgram(t, "")
			url := c.url(t)
			if c.ssh {
				// A stub named ssh guarantees no real ssh runs and no network is reached.
				stub := filepath.Join(t.TempDir(), "ssh")
				require.NoError(t, os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755))
				t.Setenv("GIT_SSH", stub)
				t.Setenv("GIT_SSH_COMMAND", "")
				require.NoError(t, os.Unsetenv("GIT_SSH_COMMAND"))
			}
			runGit(t, ws.Dir, "config", "remote.origin.url", url)
			runGit(t, ws.Dir, "config", c.key, prog)
			repo.Remote = url

			err = m.Fetch(ctx, repo)
			assertNotRun(t, marker, "fetch")
			require.Error(t, err)

			err = m.Push(ctx, repo, ws)
			assertNotRun(t, marker, "push")
			require.Error(t, err)
		})
	}
}
