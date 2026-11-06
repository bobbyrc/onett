package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// A context-size event updates the run while it is still going and is not stored as a log line.
func TestSuperviseStoresLiveContextTokens(t *testing.T) {
	env := rtNewEngine(t, rtOpts{
		script: func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
			emit(harness.Event{ContextTokens: 1_000})
			emit(harness.Event{Kind: domain.LogText, Text: "hi"})
			emit(harness.Event{ContextTokens: 2_500})
			return harness.Result{}, nil
		},
	})
	task := env.create(domain.Task{Body: "live context"})
	runs := env.firstEndedRun(task.ID)
	assert.Equal(t, int64(2_500), runs[0].ContextTokens, "the latest request's size survives a result without one")
	logs, err := env.st.ListRunLogs(env.ctx, runs[0].ID, 0, 100)
	assert.NoError(t, err)
	for _, l := range logs {
		assert.NotEmpty(t, l.Kind, "a context event is not a log line")
	}
}
