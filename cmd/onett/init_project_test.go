package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitRepoWritesTheConfigAndTheProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	useFake(t, &fakeOnboard{})

	code, out, errOut := runCLI("init", "--config", path, "--repo", "acme/app", "--no-checks")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Wrote "+path)
	assert.Contains(t, out, "Added app to "+path)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	p, ok := cfg.Projects["app"]
	require.True(t, ok)
	assert.Equal(t, "acme/app", p.Repo)
	assert.Equal(t, "main", p.DefaultBranch)
	assert.Equal(t, "supervised", p.Policy)
	assert.Empty(t, p.Setup)
	assert.Empty(t, cfg.SelfProject)
}

func TestInitProjectNamesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	useFake(t, &fakeOnboard{})

	code, _, errOut := runCLI("init", "--config", path, "--project", "tools", "--repo", "acme/app", "--no-checks")
	require.Equal(t, 0, code, errOut)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "acme/app", cfg.Projects["tools"].Repo)
	assert.NotContains(t, cfg.Projects, "app")
}

func TestInitSelfRepoSetsSelfProjectAndExplainsSelfUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	useFake(t, &fakeOnboard{})

	code, out, errOut := runCLI("init", "--config", path, "--repo", selfRepo, "--no-checks")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Self-update is on by default")
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "onett", cfg.SelfProject)
}

func TestInitRepoWithoutAChoiceKeepsTheConfigAndFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	useFake(t, &fakeOnboard{})

	code, out, errOut := runCLI("init", "--config", path, "--repo", "acme/app")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "Wrote "+path)
	assert.Contains(t, out, "Nothing was added.")
	assert.Contains(t, out, "Next steps")
	assert.Contains(t, errOut, "nothing was added")
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Projects)
}

func TestInitPathIsAUsageError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	code, _, errOut := runCLI("init", "--config", path, "--repo", "acme/app", "--path", "/src/app")
	assert.Equal(t, 2, code, errOut)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "no file is written on a usage error")
}

func TestInitChoiceFlagsNeedARepo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	for _, extra := range [][]string{{"--use-guess"}, {"--no-checks"}, {"--setup", "make"}, {"--yes"}} {
		args := append([]string{"init", "--config", path}, extra...)
		code, _, errOut := runCLI(args...)
		assert.Equal(t, 2, code, "args %v: %s", extra, errOut)
	}
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "no file is written on a usage error")
}

func TestInitWithoutRepoAddsNoProjectWithoutTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.yaml")
	f := useFake(t, &fakeOnboard{})

	code, _, errOut := runCLI("init", "--config", path)
	require.Equal(t, 0, code, errOut)
	assert.Empty(t, f.inspected)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Projects)
}
