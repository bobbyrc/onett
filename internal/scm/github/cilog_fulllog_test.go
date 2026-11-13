package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// When --log-failed prints nothing, the run's full log is read so the agent still sees the
// test output (the Web mobile layout job arrived empty).
func TestCILogFallsBackToFullLogWhenFailedLogIsEmpty(t *testing.T) {
	f := newFake()
	f.on["pr view"] = `{"number":2,"state":"OPEN","headRefName":"onett/x","statusCheckRollup":[]}`
	f.on["run list"] = `[{"databaseId":101,"conclusion":"failure","name":"ci"}]`
	f.on["run view 101 --repo"] = "" // never matched; the answer depends on the last flag below
	runner := f.runner()
	g := NewWithRunner(repo, func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--log" {
			f.calls = append(f.calls, call{args: args})
			return []byte("web\tMobile layout check\t2026-01-01T00:00:00.0Z   ✘ 1 [chromium] › overflow\n"), nil
		}
		return runner(ctx, stdin, args...)
	})

	log, err := g.CILog(context.Background(), 2, 10000)
	require.NoError(t, err)
	assert.Contains(t, log, "=== ci ===\n")
	assert.Contains(t, log, "✘ 1 [chromium] › overflow")
	assert.NotContains(t, log, "no failed-step log")
}
