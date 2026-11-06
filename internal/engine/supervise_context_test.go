package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// The run keeps the last request's prompt size apart from the summed input tokens: the chat's
// context ring reads the former.
func TestSuperviseRecordsContextTokens(t *testing.T) {
	env := rtNewEngine(t, rtOpts{
		script: costScript(harness.Result{InputTokens: 700_000, ContextTokens: 58_679, ContextWindow: 1_000_000}),
	})
	task := env.create(domain.Task{Body: "context"})
	runs := env.firstEndedRun(task.ID)
	assert.Equal(t, int64(58_679), runs[0].ContextTokens)
	assert.Equal(t, int64(1_000_000), runs[0].ContextWindow)
	assert.Equal(t, int64(700_000), runs[0].InputTokens)
}

// A log-less context event updates the run while it is still going, and is not stored as a line.
func TestSuperviseRecordsLiveContextWithoutLogLine(t *testing.T) {
	env := rtNewEngine(t, rtOpts{
		script: func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
			emit(harness.Event{Kind: domain.LogText, Text: "working"})
			emit(harness.Event{ContextTokens: 42_000})
			return harness.Result{}, nil
		},
	})
	task := env.create(domain.Task{Body: "live context"})
	runs := env.firstEndedRun(task.ID)
	assert.Equal(t, int64(42_000), runs[0].ContextTokens)
	lines, err := env.st.ListRunLogs(env.ctx, runs[0].ID, 0, 100)
	assert.NoError(t, err)
	for _, l := range lines {
		assert.NotEmpty(t, l.Kind, "the context event is not a log line")
	}
}
