package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOriginURL(t *testing.T) {
	const gh = "https://github.com/Owner/Name.git"
	cases := []struct {
		name, remote, existing, want string
	}{
		{"empty becomes the configured URL", "Owner/Name", "", gh},
		{"https with .git is kept", "Owner/Name", "https://github.com/Owner/Name.git", "https://github.com/Owner/Name.git"},
		{"https without .git is kept", "Owner/Name", "https://github.com/Owner/Name", "https://github.com/Owner/Name"},
		{"scp-style ssh with .git is kept", "Owner/Name", "git@github.com:Owner/Name.git", "git@github.com:Owner/Name.git"},
		{"scp-style ssh without .git is kept", "Owner/Name", "git@github.com:Owner/Name", "git@github.com:Owner/Name"},
		{"ssh URL with .git is kept", "Owner/Name", "ssh://git@github.com/Owner/Name.git", "ssh://git@github.com/Owner/Name.git"},
		{"ssh URL without .git is kept", "Owner/Name", "ssh://git@github.com/Owner/Name", "ssh://git@github.com/Owner/Name"},
		{"host letter case is kept as written", "Owner/Name", "git@GitHub.com:owner/name.git", "git@GitHub.com:owner/name.git"},
		{"other host is replaced", "Owner/Name", "git@evil.example:Owner/Name.git", gh},
		{"host suffix trick is replaced", "Owner/Name", "https://github.com.evil.example/Owner/Name.git", gh},
		{"other https host is replaced", "Owner/Name", "https://evil.example/Owner/Name.git", gh},
		{"other repo over ssh is replaced", "Owner/Name", "git@github.com:Owner/Other.git", gh},
		{"other owner is replaced", "Owner/Name", "git@github.com:Other/Name.git", gh},
		{"ssh port is replaced", "Owner/Name", "ssh://git@github.com:22/Owner/Name.git", gh},
		{"ssh without user is replaced", "Owner/Name", "ssh://github.com/Owner/Name.git", gh},
		{"ssh with other user is replaced", "Owner/Name", "ssh://root@github.com/Owner/Name.git", gh},
		{"https with user is replaced", "Owner/Name", "https://user@github.com/Owner/Name.git", gh},
		{"https with credentials is replaced", "Owner/Name", "https://x:token@github.com/Owner/Name.git", gh},
		{"trailing slash is replaced", "Owner/Name", "https://github.com/Owner/Name.git/", gh},
		{"fragment is replaced", "Owner/Name", "https://github.com/Owner/Name.git#x", gh},
		{"http is replaced", "Owner/Name", "http://github.com/Owner/Name.git", gh},
		{"git protocol is replaced", "Owner/Name", "git://github.com/Owner/Name.git", gh},
		{"local path is replaced", "Owner/Name", "/tmp/local.git", gh},
		{"non-GitHub project keeps its configured path for empty", "/srv/git/thing.git", "", "/srv/git/thing.git"},
		{"non-GitHub project ignores an scp-style origin", "/srv/git/thing.git", "git@github.com:Owner/Name.git", "/srv/git/thing.git"},
		{"non-GitHub project ignores a local origin", "/srv/git/thing.git", "/elsewhere.git", "/srv/git/thing.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, originURL(Repo{Name: "demo", Remote: c.remote}, c.existing))
		})
	}
}

func TestSyncOriginKeepsSSHFormOfConfiguredRepo(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))
	runGit(t, repo.Path, "config", "remote.origin.url", "git@github.com:owner/name.git")

	gh := repo
	gh.Remote = "owner/name"
	url, err := syncOrigin(ctx, gh)
	require.NoError(t, err)
	assert.Equal(t, "git@github.com:owner/name.git", url)
	assert.Equal(t, "git@github.com:owner/name.git", runGit(t, repo.Path, "config", "remote.origin.url"))
}

func TestSyncOriginReplacesOtherHostAndOtherRepo(t *testing.T) {
	for _, existing := range []string{"git@evil.example:owner/name.git", "git@github.com:owner/other.git"} {
		t.Run(existing, func(t *testing.T) {
			repo, m, _ := setup(t)
			t.Setenv("GH_TOKEN", canaryToken)
			ctx := context.Background()
			require.NoError(t, m.EnsureClone(ctx, repo))
			runGit(t, repo.Path, "config", "remote.origin.url", existing)

			gh := repo
			gh.Remote = "owner/name"
			url, err := syncOrigin(ctx, gh)
			require.NoError(t, err)
			assert.Equal(t, "https://github.com/owner/name.git", url)
			assert.Equal(t, "https://github.com/owner/name.git", runGit(t, repo.Path, "config", "remote.origin.url"))
		})
	}
}

func TestSyncOriginDropsRewritesAndPushURL(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))
	runGit(t, repo.Path, "config", "remote.origin.pushurl", "/tmp/evil.git")
	runGit(t, repo.Path, "config", "url./tmp/evil.git.insteadOf", "https://github.com/")
	runGit(t, repo.Path, "config", "url./tmp/evil.git.pushInsteadOf", "https://github.com/")

	gh := repo
	gh.Remote = "owner/name"
	_, err := syncOrigin(ctx, gh)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(repo.Path, ".git", "config"))
	require.NoError(t, err)
	text := strings.ToLower(string(data))
	assert.NotContains(t, text, "pushurl")
	assert.NotContains(t, text, "insteadof")
	assert.NotContains(t, text, "pushinsteadof")
}

func TestFetchIgnoresURLRewritePlantedFromWorktree(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/rw", "run1")
	require.NoError(t, err)

	// evil.git holds a different main branch; the rewrite below sends fetches there.
	root := t.TempDir()
	work := filepath.Join(root, "work")
	runGit(t, root, "clone", repo.Remote, work)
	writeFile(t, filepath.Join(work, "evil.txt"), "evil\n")
	runGit(t, work, "add", "-A")
	runGit(t, work, "-c", "user.name=Evil", "-c", "user.email=evil@example.com", "commit", "-m", "evil")
	evil := filepath.Join(root, "evil.git")
	runGit(t, root, "init", "--bare", "-b", "main", evil)
	runGit(t, work, "push", evil, "main")

	runGit(t, ws.Dir, "config", "url."+evil+".insteadOf", repo.Remote)
	require.NoError(t, m.Fetch(ctx, repo))

	assert.Equal(t, runGit(t, repo.Remote, "rev-parse", "refs/heads/main"),
		runGit(t, repo.Path, "rev-parse", "refs/remotes/origin/main"))
}
