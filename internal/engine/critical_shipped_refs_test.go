package engine

import (
	"path/filepath"
	"testing"
)

// This test asks whether a ref written into the project's shared clone from an agent's worktree
// can change what the deploy watch believes a release contains. The shipped check compares two
// commit IDs, so moving a remote-tracking branch such as origin/main cannot change its answer. A
// replace ref could (git replace --graft makes a release look as if it contained a commit it does
// not, and can hide a revert the same way), so onett's git ignores replace refs on every call.

// csrUnshipped pushes a branch that never reaches main and returns its commit: a merge commit no
// release contains.
func csrUnshipped(t *testing.T, r lpRepo) string {
	t.Helper()
	r.pushBranch(t, "side", map[string]string{"side.txt": "never merged\n"})
	return lpRevParse(t, r.seed, "side")
}

// csrAgentWorktree adds a worktree of the engine's clone, the way a run's worktree shares the
// clone's refs, and returns its directory.
func csrAgentWorktree(t *testing.T, r lpRepo) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "agent")
	lpGit(t, r.clone, "worktree", "add", "-b", "agent", wt, "origin/main")
	return wt
}

// A replace ref in the shared clone must not make a release look as if it contained a commit it
// does not.
func TestShippedCheckIsNotFooledByAReplaceRefInTheSharedClone(t *testing.T) {
	env, r, p := cpsEnv(t)
	unshipped := csrUnshipped(t, r)
	released := lpShippedCommit(t, r, "released.txt")
	task := lpBlockedTask(t, env, "Never shipped", unshipped, cpsFailed)

	// Control: without the replace ref the task stays blocked.
	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertBlocked(t, env, task.ID)

	// A worktree grafts the unshipped commit in as a parent of the released one.
	wt := csrAgentWorktree(t, r)
	lpGit(t, wt, "replace", "--graft", released, unshipped)

	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertBlocked(t, env, task.ID)
}
