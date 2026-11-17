package onboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Detection never follows a link: a repository can commit a symlink to /dev/zero or to a file
// outside itself, and detection runs inside the daemon.
func TestGuessIgnoresSymlinkedPackageJSON(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "pkg.json")
	require.NoError(t, os.WriteFile(target, []byte(scriptsJSON), 0o644))
	dir := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "package.json")))

	g := GuessCommands(dir)
	assert.Empty(t, g.Setup)
	assert.Empty(t, g.Checks)
}

func TestGuessIgnoresSymlinkedMakefile(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "Makefile")
	require.NoError(t, os.WriteFile(target, []byte("test:\n\tgo test ./...\n"), 0o644))
	dir := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "Makefile")))

	assert.Empty(t, GuessCommands(dir).Checks)
}

func TestGuessIgnoresPackageJSONLargerThanOneMiB(t *testing.T) {
	dir := t.TempDir()
	// Valid JSON whose scripts come first, so only the size stops the read.
	big := `{"scripts":{"test":"vitest"},"pad":"` + strings.Repeat("x", 2<<20) + `"}`
	writeTree(t, dir, map[string]string{"package.json": big})

	g := GuessCommands(dir)
	assert.Empty(t, g.Checks, "a package.json over 1 MiB is unreadable, so no scripts")
}

func TestGuessSkipsSymlinkedSubdirectory(t *testing.T) {
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"go.mod": "module x\n"})
	dir := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "linked")))

	g := GuessCommands(dir)
	assert.Empty(t, g.Setup)
	assert.Empty(t, g.Checks)
}

// A subdirectory name goes into a shell `cd` command, so names that need quoting are skipped.
func TestGuessSkipsSubdirectoryNameWithSpace(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"my app/package.json": `{"scripts":{"test":"vitest"}}`})
	assert.Empty(t, GuessCommands(dir).Checks)

	writeTree(t, dir, map[string]string{"web/package-lock.json": "{}", "web/package.json": "{}"})
	var names []string
	for _, c := range GuessCommands(dir).Setup {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"web: npm ci"}, names)
}

// Case does not matter for GitHub repository names, so a configured project blocks the same
// repository spelled another way, before any gh call or clone.
func TestInspectCaseInsensitiveGitHubNameIsExists(t *testing.T) {
	f := &fakeGH{}
	svc, dataDir := newTestService(t, f)
	_, err := svc.Inspect(context.Background(), Request{
		Repo: "acme/app", DataDir: dataDir, Existing: map[string]string{"app": "Acme/App"},
	}, nil)
	assert.ErrorIs(t, err, ErrExists)
	assert.Empty(t, f.calls)
	assert.NoDirExists(t, filepath.Join(dataDir, "repos"))
}

func TestCheckedMatchesGitHubNameIgnoringCase(t *testing.T) {
	svc, _ := newTestService(t, &fakeGH{})
	svc.checked["app"] = Result{Name: "app", Repo: "acme/app"}
	_, ok := svc.Checked("Acme/App")
	assert.True(t, ok)
}

func TestReposProjectMatchIgnoresGitHubCase(t *testing.T) {
	f := &fakeGH{signedIn: true, answers: map[string]string{
		"api user":  "bcraig",
		"repo list": `[{"nameWithOwner":"acme/pimlo","defaultBranchRef":{"name":"main"},"pushedAt":"2026-03-01T00:00:00Z"}]`,
	}}
	svc, _ := newTestService(t, f)
	out := svc.Repos(context.Background(), map[string]string{"pimlo": "Acme/Pimlo"})
	require.Len(t, out.Repos, 1)
	require.NotNil(t, out.Repos[0].Project)
	assert.Equal(t, "pimlo", *out.Repos[0].Project)
}

// A local path is compared exactly: the file system may be case-sensitive.
func TestLocalPathMatchIsExact(t *testing.T) {
	svc, _ := newTestService(t, &fakeGH{})
	svc.checked["x"] = Result{Name: "x", Repo: "/srv/Git/X.git"}
	_, ok := svc.Checked("/srv/git/x.git")
	assert.False(t, ok)
}

// removeCopy is the cleanup a failed or cancelled check runs on a copy it made. Forcing a fetch to
// fail right after a clone is not reliable with a local repository, so the cleanup is tested
// directly.
func TestRemoveCopyDeletesCopyAndForgets(t *testing.T) {
	svc, dataDir := newTestService(t, &fakeGH{})
	path := filepath.Join(dataDir, "repos", "app")
	require.NoError(t, os.MkdirAll(filepath.Join(path, ".git"), 0o755))
	svc.made["app"] = path
	svc.checked["app"] = Result{Name: "app", Repo: "acme/app"}

	svc.removeCopy("app", path)

	assert.NoDirExists(t, path)
	_, ok := svc.Checked("acme/app")
	assert.False(t, ok)
	assert.ErrorIs(t, svc.Discard("app", nil), ErrNotFound)
}
