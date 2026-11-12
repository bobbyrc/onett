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

// canaryToken stands for a credential in onett's environment. No planted program may ever see it.
const canaryToken = "canary-credential-7f3a"

// plantProgram writes a shell program that records its environment in marker, then runs body.
// It returns the program's path and the marker's path. The program never reads stdin unless body
// does, because a program blocked on stdin can hang git.
func plantProgram(t *testing.T, body string) (prog, marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "ran")
	prog = filepath.Join(dir, "prog.sh")
	content := "#!/bin/sh\nenv >> '" + marker + "'\n" + body + "\n"
	require.NoError(t, os.WriteFile(prog, []byte(content), 0o755))
	return prog, marker
}

// assertNotRun fails the test when the planted program left its marker behind.
func assertNotRun(t *testing.T, marker, step string) {
	t.Helper()
	data, err := os.ReadFile(marker)
	if err == nil {
		t.Fatalf("%s ran the planted program (it saw onett's credential: %v)", step,
			strings.Contains(string(data), canaryToken))
	}
}

// pushToMain adds a commit to origin's main from a human's clone.
func pushToMain(t *testing.T, repo Repo, file, content string) {
	t.Helper()
	// This git stands in for a person on another machine: it must not read the global config a
	// test may have planted for onett.
	human := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	clone := filepath.Join(t.TempDir(), "main-clone")
	human(filepath.Dir(clone), "clone", repo.Remote, clone)
	writeFile(t, filepath.Join(clone, file), content)
	human(clone, "add", "-A")
	human(clone, "-c", "user.name=Human", "-c", "user.email=human@example.com", "commit", "-m", "main work")
	human(clone, "push", "origin", "main")
}

// exercise runs every git path onett uses on a worktree whose clone has been planted. After each
// step it checks the marker first, so a failure names the program rather than a side effect.
func exercise(t *testing.T, m *Manager, repo Repo, ws Workspace, marker string) {
	t.Helper()
	ctx := context.Background()

	writeFile(t, filepath.Join(ws.Dir, ".gitattributes"), "* filter=evil merge=evil diff=evil\n")
	rest := strings.SplitN(numberedLines(10), "\n", 2)[1]
	writeFile(t, filepath.Join(ws.Dir, "pkg", "a.txt"), "changed first line\n"+rest)
	committed, err := m.Commit(ctx, ws, "work")
	assertNotRun(t, marker, "commit")
	require.NoError(t, err)
	require.True(t, committed)

	err = m.Push(ctx, repo, ws)
	assertNotRun(t, marker, "push")
	require.NoError(t, err)

	err = m.Fetch(ctx, repo)
	assertNotRun(t, marker, "fetch")
	require.NoError(t, err)

	_, err = m.Diff(ctx, repo, "main", ws.Branch)
	assertNotRun(t, marker, "diff")
	require.NoError(t, err)

	pushToMain(t, repo, "pkg/a.txt", numberedLines(10)+"added on main\n")
	conflicts, err := m.MergeBase(ctx, repo, ws)
	assertNotRun(t, marker, "merge")
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	ws2, err := m.Prepare(ctx, repo, "onett/second", "run2")
	assertNotRun(t, marker, "prepare")
	require.NoError(t, err)

	err = m.Remove(ctx, repo, ws2)
	assertNotRun(t, marker, "remove")
	require.NoError(t, err)
}

// plantCase is one git setting that makes git run a program, written into the shared clone.
type plantCase struct {
	name  string
	plant func(t *testing.T, repo Repo, ws Workspace, seed, prog string)
}

func TestPlantedGitSettingsDoNotRunPrograms(t *testing.T) {
	cases := []plantCase{
		{name: "core.fsmonitor", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "core.fsmonitor", prog)
		}},
		{name: "filter driver", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "filter.evil.clean", prog)
			runGit(t, ws.Dir, "config", "filter.evil.smudge", prog)
		}},
		{name: "merge driver", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "merge.evil.driver", prog+" %A")
		}},
		{name: "diff driver", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "diff.evil.textconv", prog)
			runGit(t, ws.Dir, "config", "diff.evil.command", prog)
			runGit(t, ws.Dir, "config", "diff.external", prog)
		}},
		{name: "gpg.program with commit.gpgsign", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "gpg.program", prog)
			runGit(t, ws.Dir, "config", "commit.gpgsign", "true")
		}},
		{name: "include.path", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			inc := filepath.Join(ws.Dir, ".inc")
			writeFile(t, inc, "[core]\n\tfsmonitor = "+prog+"\n[filter \"evil\"]\n\tclean = "+prog+"\n")
			runGit(t, ws.Dir, "config", "include.path", inc)
		}},
		{name: "per-worktree config file", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "extensions.worktreeConfig", "true")
			runGit(t, ws.Dir, "config", "--worktree", "filter.evil.clean", prog)
		}},
		{name: "core.alternateRefsCommand", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "core.alternateRefsCommand", prog)
			alt := filepath.Join(repo.Path, ".git", "objects", "info", "alternates")
			writeFile(t, alt, filepath.Join(seed, ".git", "objects")+"\n")
		}},
		{name: "remote upload-pack and receive-pack", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "remote.origin.uploadpack", prog)
			runGit(t, ws.Dir, "config", "remote.origin.receivepack", prog)
		}},
		{name: "ext transport url", plant: func(t *testing.T, repo Repo, ws Workspace, seed, prog string) {
			runGit(t, ws.Dir, "config", "protocol.ext.allow", "always")
			runGit(t, ws.Dir, "config", "remote.origin.url", "ext::"+prog+" %S")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, m, seed := setup(t)
			t.Setenv("GH_TOKEN", canaryToken)
			ctx := context.Background()
			ws, err := m.Prepare(ctx, repo, "onett/h", "run1")
			require.NoError(t, err)
			prog, marker := plantProgram(t, "")
			c.plant(t, repo, ws, seed, prog)
			exercise(t, m, repo, ws, marker)
		})
	}
}

func TestNestedRepositoryConfigDoesNotRunPrograms(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/nested", "run1")
	require.NoError(t, err)
	prog, marker := plantProgram(t, "")

	sub := filepath.Join(ws.Dir, "sub")
	runGit(t, ws.Dir, "init", "sub")
	writeFile(t, filepath.Join(sub, ".gitattributes"), "* filter=evil\n")
	writeFile(t, filepath.Join(sub, "f.txt"), "one\n")
	runGit(t, sub, "add", "-A")
	runGit(t, sub, "-c", "user.name=A", "-c", "user.email=a@example.com", "commit", "-m", "nested")
	runGit(t, sub, "config", "core.fsmonitor", prog)
	runGit(t, sub, "config", "filter.evil.clean", prog)

	// Stage the nested repository in the worktree as an agent could. git warns, so its error is ignored.
	add := exec.Command("git", "add", "sub")
	add.Dir = ws.Dir
	_ = add.Run()
	writeFile(t, filepath.Join(sub, "f.txt"), "one\ntwo and more\n")

	writeFile(t, filepath.Join(ws.Dir, "README.md"), "branch side\n")
	_, err = m.Commit(ctx, ws, "first")
	assertNotRun(t, marker, "first commit")
	require.NoError(t, err)

	writeFile(t, filepath.Join(ws.Dir, "README.md"), "branch side again\n")
	_, err = m.Commit(ctx, ws, "second")
	assertNotRun(t, marker, "second commit")
	require.NoError(t, err)

	pushToMain(t, repo, "README.md", "main side\n")
	conflicts, err := m.MergeBase(ctx, repo, ws)
	assertNotRun(t, marker, "conflicting merge")
	require.NoError(t, err)
	assert.Equal(t, []string{"README.md"}, conflicts)
}

func TestLocalGitCommandsGetNoCredentials(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	prog, marker := plantProgram(t, "cat")

	// The owner's own global filter is a program git legitimately runs; it still must get no credential.
	g := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, g, "[filter \"canary\"]\n\tclean = "+prog+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", g)

	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/env", "run1")
	require.NoError(t, err)
	writeFile(t, filepath.Join(ws.Dir, ".gitattributes"), "*.txt filter=canary\n")
	writeFile(t, filepath.Join(ws.Dir, "pkg", "a.txt"), "changed\n")
	_, err = m.Commit(ctx, ws, "work")
	require.NoError(t, err)

	data, err := os.ReadFile(marker)
	require.NoError(t, err, "the owner's filter should have run")
	assert.NotContains(t, string(data), canaryToken)
	assert.Contains(t, string(data), "GIT_TERMINAL_PROMPT=0")
}

func TestHardeningOverridesWinOverOtherConfigFiles(t *testing.T) {
	repo, m, _ := setup(t)
	t.Setenv("GH_TOKEN", canaryToken)
	prog, marker := plantProgram(t, "")
	g := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, g, "[core]\n\tfsmonitor = "+prog+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", g)

	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/ov", "run1")
	assertNotRun(t, marker, "prepare")
	require.NoError(t, err)
	exercise(t, m, repo, ws, marker)
}
