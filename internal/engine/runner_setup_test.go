package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// A deterministic setup failure is not retried: the agent starts with the failing command and its
// output in the prompt. When setup still fails after the agent's turn, the task is blocked with
// the real error and uses no further attempts.
func TestRunnerFailingSetupGoesToAgentThenBlocks(t *testing.T) {
	var starts atomic.Int32
	var prompt atomic.Value
	script := func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		starts.Add(1)
		prompt.Store(spec.Prompt)
		return hfake.DefaultScript(ctx, spec, emit)
	}
	env := rtNewEngine(t, rtOpts{maxAttempts: 3, script: script, projectExtra: map[string]string{
		"demo": `    setup: [{name: prep, run: "echo boom; exit 3"}]` + "\n",
	}})
	task := env.create(domain.Task{Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "setup failed: prep")
	assert.Contains(t, got.BlockedReason, "boom", "the block carries the real error")
	assert.NotContains(t, got.BlockedReason, "gave up", "a setup failure is not retried")
	assert.EqualValues(t, 1, starts.Load(), "the agent gets exactly one turn")
	assert.Contains(t, prompt.Load(), "fix this first")
	assert.Contains(t, prompt.Load(), "boom")
	assert.Empty(t, env.scm.PRs, "no pull request is opened while setup fails")
}

// A network-like setup failure waits out the backoff and then re-queues; the agent never starts.
func TestRunnerTransientSetupFailureBacksOff(t *testing.T) {
	old := setupBackoff
	setupBackoff = 300 * time.Millisecond
	t.Cleanup(func() { setupBackoff = old })
	var starts atomic.Int32
	script := func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		starts.Add(1)
		return harness.Result{FinalText: "done"}, nil
	}
	env := rtNewEngine(t, rtOpts{maxAttempts: 2, script: script, projectExtra: map[string]string{
		"demo": `    setup: [{name: prep, run: "echo npm ERR! code ETIMEDOUT; exit 1"}]` + "\n",
	}})
	start := time.Now()
	task := env.create(domain.Task{Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "gave up after 2 attempts")
	assert.GreaterOrEqual(t, time.Since(start), 2*setupBackoff, "each failed attempt waits the backoff")
	assert.Zero(t, starts.Load(), "the agent never starts on a transient failure")
}

func TestTransientFailure(t *testing.T) {
	assert.True(t, transientFailure("npm ERR! code ETIMEDOUT"))
	assert.True(t, transientFailure("getaddrinfo EAI_AGAIN registry.npmjs.org"))
	assert.False(t, transientFailure("npm ERR! code EJSONPARSE\nUnexpected token <<<<<<<"))
}

// With conflicts in the tree, setup is skipped and the agent is told to resolve first and run
// the setup commands itself.
func TestSetupDeferredSectionListsCommands(t *testing.T) {
	got := setupDeferredSection([]config.Check{{Name: "deps", Run: "npm ci"}})
	assert.Contains(t, got, "Resolve the conflicts first")
	assert.Contains(t, got, "`npm ci`")
}

// Setup that passes lets the agent run and the task publish as usual.
func TestRunnerPassingSetupPublishes(t *testing.T) {
	env := rtNewEngine(t, rtOpts{projectExtra: map[string]string{
		"demo": `    setup: [{name: prep, run: "test -f README.md"}]` + "\n",
	}})
	task := env.create(domain.Task{Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskReview)
	assert.NotZero(t, got.PRNumber, "a passing setup lets the agent run and the change publish")
}

// Setup and checks run with the agent's environment: an onett-only variable is not visible to
// them, and the variables the agent gets (ONETT_TASK_ID) are.
func TestRunnerSetupAndChecksUseAgentEnvironment(t *testing.T) {
	t.Setenv("ONETT_CHECK_TEST_SECRET", "s3cr3t-value")
	env := rtNewEngine(t, rtOpts{projectExtra: map[string]string{
		"demo": `    setup: [{name: env, run: "test -z \"$ONETT_CHECK_TEST_SECRET\" && test \"$ONETT_TASK_ID\" != \"\""}]` + "\n" +
			`    checks: [{name: env, run: "test -z \"$ONETT_CHECK_TEST_SECRET\" && test \"$ONETT_TASK_ID\" != \"\""}]` + "\n",
	}})
	task := env.create(domain.Task{Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskReview)
	assert.NotZero(t, got.PRNumber, "setup and checks see the agent's environment, not onett's secret")
}
