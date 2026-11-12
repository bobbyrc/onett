package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests ask whether a ref written into the project's shared clone from an agent's worktree
// can change what the deploy watch believes a release contains.

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

// The shipped check asks about two commit IDs, so moving a remote-tracking branch in the shared
// clone after onett's fetch does not change the answer.
func TestShippedQuestionsAreNotMovedByARewrittenRemoteBranch(t *testing.T) {
	env, r, p := cpsEnv(t)
	unshipped := csrUnshipped(t, r)
	released := lpShippedCommit(t, r, "released.txt")
	repo := env.e.repoFor(p)
	require.NoError(t, env.e.d.Workspaces.Fetch(env.ctx, repo))

	// Between onett's fetch and its question, a worktree points origin/main at the unshipped commit.
	wt := csrAgentWorktree(t, r)
	lpGit(t, wt, "update-ref", "refs/remotes/origin/main", unshipped)

	shipped, err := env.e.d.Workspaces.IsAncestor(env.ctx, repo, unshipped, released)
	require.NoError(t, err)
	assert.False(t, shipped, "the release does not contain the commit, whatever origin/main says")
}

// A replace ref in the shared clone must not make a release look as if it contained a commit it
// does not.
func TestShippedCheckIsNotFooledByAReplaceRefInTheSharedClone(t *testing.T) {
	if os.Getenv("ONETT_RUN_EXPOSING") == "" {
		t.Skip("exposes: a replace ref written into the shared clone from an agent's worktree (git replace --graft) makes the deploy watch's ancestry check say a release contains a commit it does not, so a task blocked by a failed release is marked done although its work never shipped; the same trick can hide a revert from the revert check")
	}
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
