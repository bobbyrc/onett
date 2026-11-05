package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// reportThenSilent writes rr to the outbox and then emits nothing until it is killed, like
// Claude Code waiting on a background command after the agent has reported.
func reportThenSilent(rr domain.RunResult) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		if err := hfake.WriteResult(spec.Workdir, rr); err != nil {
			return harness.Result{IsError: true}, err
		}
		<-ctx.Done()
		return harness.Result{IsError: true}, nil
	}
}

func TestStalledRunKeepsAgentReport(t *testing.T) {
	long := strings.Repeat("x", 800)
	env := rtNewEngine(t, rtOpts{script: reportThenSilent(domain.RunResult{
		Status:  domain.ResultBlocked,
		Summary: "every go command hangs " + long,
	})})
	task := env.create(domain.Task{Body: "report then stall"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "stalled: no output for 2s, after the agent reported: blocked: every go command hangs")
	assert.Less(t, len(got.BlockedReason), 700, "the summary is truncated")

	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunStalled, runs[0].Status)
	assert.Contains(t, runs[0].Error, "after the agent reported: blocked")
}

func TestStalledRunWithoutReportKeepsPlainReason(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "[fake:stall]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Equal(t, "stalled: no output for 2s", got.BlockedReason)
}
