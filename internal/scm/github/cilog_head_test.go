package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The log must come from the PR's current head commit, name it, and never be a blank block.
func TestCILogPinsToHeadCommitAndExplainsEmptyLogs(t *testing.T) {
	f := newFake()
	f.on["pr view"] = `{"number":2,"state":"OPEN","headRefName":"onett/x","headRefOid":"0123456789abcdef","statusCheckRollup":[]}`
	f.on["run list"] = `[{"databaseId":101,"conclusion":"failure","name":"ci"}]`
	f.on["run view 101"] = "\n"
	g := NewWithRunner(repo, f.runner())

	log, err := g.CILog(context.Background(), 2, 10000)
	require.NoError(t, err)
	assert.Equal(t, []string{"run", "list", "--repo", repo, "--branch", "onett/x", "--commit", "0123456789abcdef",
		"--json", "databaseId,conclusion,name", "--limit", "10"}, f.calls[1].args)
	assert.Contains(t, log, "=== ci @ 0123456 ===\n")
	assert.Contains(t, log, "no failed-step log")
}
