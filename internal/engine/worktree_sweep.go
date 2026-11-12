package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
	"github.com/bobbyrc/onett/internal/workspace"
)

// sweepWorktrees removes worktrees that runs left behind in project p's directory. runTask
// removes a run's worktree only when the run publishes or finishes cleanly, so a run that is
// re-queued, fails, is blocked, stalls, times out or is cancelled keeps its worktree, and a
// project's setup can leave about 100 MB of dependencies in each. A leftover worktree may hold
// the only copy of work the agent did not commit, so it is removed only once its task cannot
// run again (see taskMayRunAgain). Until then it stays, and the task's next run saves that work
// as a commit when it takes the branch (workspace.Prepare). A cancelled task is the one in
// between: a person can still retry it, so its worktree stays while it holds uncommitted work
// and goes when it holds none. Problems are logged and never
// returned: this is housekeeping, and it must not fail a run or delay the engine's start.
func (e *Engine) sweepWorktrees(ctx context.Context, p *config.Project) {
	dir := filepath.Join(e.d.Workspaces.Root, p.Name)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		e.d.Logger.Warn("read worktree directory", "dir", dir, "err", err)
		return
	}
	for _, ent := range entries {
		if ctx.Err() != nil {
			return // shutting down; the next start sweeps again
		}
		if !ent.IsDir() {
			continue
		}
		runID := ent.Name()
		path := filepath.Join(dir, runID)
		run, leftover := e.leftoverRun(ctx, runID, path)
		if !leftover {
			continue
		}
		err = e.d.Workspaces.Remove(ctx, e.repoFor(p), workspace.Workspace{Dir: path})
		if err != nil {
			// Git refuses to remove a directory it no longer knows as a worktree. Such a
			// directory is plain leftover data, and it is removed directly.
			removed, dirErr := removeUnregisteredDir(dir, runID)
			switch {
			case dirErr != nil:
				e.d.Logger.Warn("remove leftover directory", "dir", path, "err", dirErr)
			case removed:
				e.d.Logger.Info("removed leftover directory", "run", runID, "task", run.TaskID, "dir", path)
			default:
				e.d.Logger.Warn("remove leftover worktree", "dir", path, "err", err)
			}
			continue
		}
		e.d.Logger.Info("removed leftover worktree", "run", runID, "task", run.TaskID, "dir", path)
	}
}

// leftoverRun reports whether the worktree of run runID may be removed, and returns the run
// row when it is known. A directory that is not a known run belongs to nothing onett can
// judge, so it is kept. A run that has not ended may still be using its worktree, so it is kept
// as well. A live run is kept even when its row says it ended: runTask sets EndedAt before it
// publishes or removes its own worktree, so the row alone cannot tell a finished run from one
// still finishing.
// An ended run's worktree is leftover only when its task cannot run again, and for a cancelled
// task only when the worktree at dir holds no uncommitted work.
func (e *Engine) leftoverRun(ctx context.Context, runID, dir string) (domain.Run, bool) {
	e.runsMu.Lock()
	_, live := e.active[runID]
	e.runsMu.Unlock()
	if live {
		return domain.Run{}, false
	}
	run, err := e.d.Store.GetRun(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Run{}, false
	}
	if err != nil {
		e.d.Logger.Warn("read run for worktree sweep", "run", runID, "err", err)
		return domain.Run{}, false
	}
	if run.EndedAt == nil {
		return run, false
	}
	task, err := e.d.Store.GetTask(ctx, run.TaskID)
	if err != nil {
		e.d.Logger.Warn("read task for worktree sweep", "run", runID, "task", run.TaskID, "err", err)
		return run, false
	}
	if task.Status == domain.TaskCancelled {
		// A person can still retry a cancelled task, and that run saves this work as a commit.
		// When git cannot say (the directory is no longer a worktree), no later run could save
		// anything from it, so it is removed like any other leftover.
		dirty, err := e.d.Workspaces.HasUncommittedWork(ctx, workspace.Workspace{Dir: dir})
		if err == nil && dirty {
			return run, false
		}
	}
	return run, !taskMayRunAgain(task.Status)
}

// taskMayRunAgain reports whether a task in status s can still get a run on its branch. Only a
// task that has landed (done, merged, deploying) or was cancelled cannot. Every other status
// counts as "may run again", and so does a status added later: keeping a worktree too long
// costs disk, removing one too early loses work.
//
// A person can retry a cancelled task, so leftoverRun keeps a cancelled task's worktree while it
// holds uncommitted work.
// shortcut: such a worktree stays until a person retries the task or deletes the directory;
// give it an age limit if these pile up.
func taskMayRunAgain(s domain.TaskStatus) bool {
	switch s {
	case domain.TaskDone, domain.TaskMerged, domain.TaskDeploying, domain.TaskCancelled:
		return false
	}
	return true
}

// removeUnregisteredDir deletes the directory of run runID in projectDir when it is no longer a
// git worktree. A worktree has a .git entry, and that case is left alone: git owns it, and
// removing it behind git's back would corrupt the clone's bookkeeping. It reports whether it
// removed the directory. The path is checked against projectDir before anything is deleted, so
// a surprising run ID can never reach outside the project's directory.
func removeUnregisteredDir(projectDir, runID string) (bool, error) {
	path := filepath.Join(projectDir, runID)
	if filepath.Dir(path) != filepath.Clean(projectDir) || filepath.Base(path) != runID {
		return false, fmt.Errorf("refusing to remove %s: not directly under %s", path, projectDir)
	}
	_, err := os.Lstat(filepath.Join(path, ".git"))
	if !errors.Is(err, fs.ErrNotExist) {
		return false, nil // still a worktree, or unreadable: not ours to delete
	}
	if err := os.RemoveAll(path); err != nil {
		return false, fmt.Errorf("remove %s: %w", path, err)
	}
	return true, nil
}
