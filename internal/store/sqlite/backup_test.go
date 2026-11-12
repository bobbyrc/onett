package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

func seedBackupSource(t *testing.T, src string) (taskIDs []string, triggerID string) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(src)
	require.NoError(t, err)
	next := triggerBase.Add(time.Hour)
	for _, title := range []string{"first", "second"} {
		tk, err := s.CreateTask(ctx, domain.Task{Project: "p", Title: title, Status: domain.TaskQueued})
		require.NoError(t, err)
		taskIDs = append(taskIDs, tk.ID)
	}
	tr, err := s.CreateTrigger(ctx, domain.Trigger{
		Name: "nightly", Kind: domain.TriggerSchedule, Project: "p", Prompt: "do it",
		Enabled: true, Cron: "0 5 * * *", TimeZone: "America/Chicago", NextRunAt: &next,
	})
	require.NoError(t, err)
	// Closing first proves a backup works on a cleanly closed database too.
	require.NoError(t, s.Close())
	return taskIDs, tr.ID
}

func TestBackupRoundTrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "onett.db")
	taskIDs, triggerID := seedBackupSource(t, src)

	dst := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, Backup(context.Background(), src, dst))
	assert.NoFileExists(t, dst+".partial")

	s, err := Open(dst)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()

	tasks, err := s.ListTasks(ctx, store.TaskFilter{})
	require.NoError(t, err)
	titles := make([]string, 0, len(tasks))
	ids := make([]string, 0, len(tasks))
	for _, tk := range tasks {
		titles = append(titles, tk.Title)
		ids = append(ids, tk.ID)
	}
	assert.ElementsMatch(t, []string{"first", "second"}, titles)
	assert.ElementsMatch(t, taskIDs, ids)

	triggers, err := s.ListTriggers(ctx)
	require.NoError(t, err)
	require.Len(t, triggers, 1)
	assert.Equal(t, triggerID, triggers[0].ID)
	assert.Equal(t, "nightly", triggers[0].Name)
	assert.Equal(t, "0 5 * * *", triggers[0].Cron)
}

func TestBackupRefusesToOverwrite(t *testing.T) {
	src := filepath.Join(t.TempDir(), "onett.db")
	seedBackupSource(t, src)

	dst := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, os.WriteFile(dst, []byte("keep me"), 0o644))

	err := Backup(context.Background(), src, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(got))
	assert.NoFileExists(t, dst+".partial")
}

func TestBackupFileModeIs0600(t *testing.T) {
	src := filepath.Join(t.TempDir(), "onett.db")
	seedBackupSource(t, src)

	dst := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, Backup(context.Background(), src, dst))

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestBackupWhileAnotherConnectionWrites(t *testing.T) {
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "onett.db")
	s, err := Open(src)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	for i := range 50 {
		_, err := s.CreateTask(ctx, domain.Task{Project: "p", Title: "seed", Status: domain.TaskQueued})
		require.NoError(t, err, "seed %d", i)
	}

	// The writer goroutine must not call require/assert: it stores its error for the test
	// goroutine to read after wg.Wait.
	var writes atomic.Int64
	var writeErr error
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := s.CreateTask(ctx, domain.Task{Project: "p", Title: "live", Status: domain.TaskQueued}); err != nil {
				writeErr = err
				return
			}
			writes.Add(1)
		}
	}()

	waitFor := func(target int64) {
		deadline := time.Now().Add(10 * time.Second)
		for writes.Load() < target {
			if time.Now().After(deadline) {
				t.Fatalf("writer stalled at %d writes, wanted %d", writes.Load(), target)
			}
			time.Sleep(time.Millisecond)
		}
	}

	waitFor(10)
	dst := filepath.Join(t.TempDir(), "copy.db")
	before := writes.Load()
	require.NoError(t, Backup(ctx, src, dst))
	waitFor(before + 5)
	close(stop)
	wg.Wait()
	require.NoError(t, writeErr)

	db, err := sql.Open("sqlite", "file:"+dst)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var result string
	require.NoError(t, db.QueryRow("PRAGMA integrity_check").Scan(&result))
	assert.Equal(t, "ok", result)

	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&n))
	assert.GreaterOrEqual(t, n, 60)
	assert.LessOrEqual(t, n, 50+int(writes.Load()))

	copied, err := Open(dst)
	require.NoError(t, err)
	require.NoError(t, copied.Close())
}

func TestBackupMissingSource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "missing.db")
	dst := filepath.Join(t.TempDir(), "copy.db")

	err := Backup(context.Background(), src, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database at")
	assert.NoFileExists(t, dst)
	assert.NoFileExists(t, src)
}

func TestBackupFailureLeavesNoFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "notdb.db")
	require.NoError(t, os.WriteFile(src, bytes.Repeat([]byte("not a database\n"), 400), 0o600))
	dst := filepath.Join(t.TempDir(), "copy.db")

	err := Backup(context.Background(), src, dst)
	require.Error(t, err)
	assert.NoFileExists(t, dst)
	assert.NoFileExists(t, dst+".partial")
	assert.NoFileExists(t, dst+".partial-journal")
}
