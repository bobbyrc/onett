package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// The run keeps the last request's prompt size apart from the summed input tokens: the chat's
// context ring reads the former.
func TestSuperviseRecordsContextTokens(t *testing.T) {
	env := rtNewEngine(t, rtOpts{
		script: costScript(harness.Result{InputTokens: 700_000, ContextTokens: 58_679}),
	})
	task := env.create(domain.Task{Body: "context"})
	runs := env.firstEndedRun(task.ID)
	assert.Equal(t, int64(58_679), runs[0].ContextTokens)
	assert.Equal(t, int64(700_000), runs[0].InputTokens)
}
