package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

// backupFixture makes a config and a database with one task and one trigger in temp directories,
// and returns the config path, the data directory and the task's ID. It clears the variables that
// mark an agent run, because onett runs this suite inside agent runs too.
func backupFixture(t *testing.T) (cfgPath, dataDir, taskID string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ONETT_RUN_ID", "")
	t.Setenv("ONETT_ROLE", "")
	dataDir = t.TempDir()
	cfgPath = filepath.Join(t.TempDir(), "onett.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf("server:\n  data_dir: %s\n", dataDir)), 0o600))

	st, err := sqlite.Open(filepath.Join(dataDir, "onett.db"))
	require.NoError(t, err)
	task, err := st.CreateTask(context.Background(), domain.Task{Project: "p", Title: "kept", Status: domain.TaskQueued})
	require.NoError(t, err)
	_, err = st.CreateTrigger(context.Background(), domain.Trigger{Name: "nightly", Kind: domain.TriggerSchedule, Project: "p", Prompt: "do it", Enabled: true, Cron: "0 5 * * *", TimeZone: "America/Chicago"})
	require.NoError(t, err)
	require.NoError(t, st.Close())
	return cfgPath, dataDir, task.ID
}

func TestBackupCommandWritesACopyThatOpens(t *testing.T) {
	cfgPath, _, taskID := backupFixture(t)
	dst := filepath.Join(t.TempDir(), "copy.db")

	var out, errOut bytes.Buffer
	code := run([]string{"backup", dst, "--config", cfgPath}, &out, &errOut)
	require.Equal(t, 0, code, errOut.String())
	assert.Contains(t, out.String(), "Backed up")
	assert.Contains(t, out.String(), dst)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	copied, err := sqlite.Open(dst)
	require.NoError(t, err)
	t.Cleanup(func() { _ = copied.Close() })
	tasks, err := copied.ListTasks(context.Background(), store.TaskFilter{})
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, taskID, tasks[0].ID)
	assert.Equal(t, "kept", tasks[0].Title)
	triggers, err := copied.ListTriggers(context.Background())
	require.NoError(t, err)
	require.Len(t, triggers, 1)
	assert.Equal(t, "nightly", triggers[0].Name)
}

func TestBackupCommandRefusesInsideAgentRun(t *testing.T) {
	for _, name := range []string{"ONETT_RUN_ID", "ONETT_ROLE"} {
		t.Run(name, func(t *testing.T) {
			cfgPath, _, _ := backupFixture(t)
			t.Setenv(name, "x")
			dst := filepath.Join(t.TempDir(), "copy.db")

			var out, errOut bytes.Buffer
			code := run([]string{"backup", dst, "--config", cfgPath}, &out, &errOut)
			assert.Equal(t, 1, code)
			assert.Contains(t, errOut.String(), "inside an agent run")
			assert.NoFileExists(t, dst)
		})
	}
}

func TestBackupCommandRefusesExistingFile(t *testing.T) {
	cfgPath, _, _ := backupFixture(t)
	dst := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, os.WriteFile(dst, []byte("keep me"), 0o644))

	var out, errOut bytes.Buffer
	code := run([]string{"backup", dst, "--config", cfgPath}, &out, &errOut)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut.String(), "already exists")
	content, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(content))
}

func TestBackupCommandRefusesDestinationInsideGitRepo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, repo string)
	}{
		{"repository", func(t *testing.T, repo string) {
			require.NoError(t, os.Mkdir(filepath.Join(repo, ".git"), 0o700))
		}},
		{"worktree", func(t *testing.T, repo string) {
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /elsewhere\n"), 0o600))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgPath, _, _ := backupFixture(t)
			repo := t.TempDir()
			tc.setup(t, repo)
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "sub", "deeper"), 0o700))
			dst := filepath.Join(repo, "sub", "deeper", "copy.db")

			var out, errOut bytes.Buffer
			code := run([]string{"backup", dst, "--config", cfgPath}, &out, &errOut)
			assert.Equal(t, 1, code)
			assert.Contains(t, errOut.String(), "inside the git repository or worktree")
			assert.NoFileExists(t, dst)
		})
	}
}

func TestBackupCommandNeedsOneArgument(t *testing.T) {
	cfgPath, _, _ := backupFixture(t)

	var out, errOut bytes.Buffer
	code := run([]string{"backup", "--config", cfgPath}, &out, &errOut)
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut.String(), "usage: onett backup FILE")
}

func TestBackupCommandWithoutDatabase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ONETT_RUN_ID", "")
	t.Setenv("ONETT_ROLE", "")
	dataDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "onett.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf("server:\n  data_dir: %s\n", dataDir)), 0o600))
	dst := filepath.Join(t.TempDir(), "copy.db")

	var out, errOut bytes.Buffer
	code := run([]string{"backup", dst, "--config", cfgPath}, &out, &errOut)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut.String(), "no database at")
	assert.NoFileExists(t, dst)
	assert.NoFileExists(t, filepath.Join(dataDir, "onett.db"))
}

func TestBackupCommandMissingDirectory(t *testing.T) {
	cfgPath, _, _ := backupFixture(t)
	dst := filepath.Join(t.TempDir(), "no-such-dir", "copy.db")

	var out, errOut bytes.Buffer
	code := run([]string{"backup", dst, "--config", cfgPath}, &out, &errOut)
	assert.Equal(t, 1, code)
	assert.NotEmpty(t, errOut.String())
	assert.NoFileExists(t, dst)
}
