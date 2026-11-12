package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
)

func TestProblemsKeysAreUnique(t *testing.T) {
	env := pbEnv(t)
	lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "a", Status: domain.TaskBlocked,
		BlockedReason: "agent needs a human:  which colour?", BlockedAt: pbAt(env.now.Add(-time.Hour))})
	lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "b", Status: domain.TaskBlocked,
		BlockedReason: "agent needs a human: which colour?\n", BlockedAt: pbAt(env.now.Add(-2 * time.Hour))})

	got := pbProblems(t, env, api.ProblemBlockedTasks, "alpha")
	require.Len(t, got, 1, "reasons that differ only in whitespace are one cause")
	assert.Equal(t, 2, got[0].Count)

	for i, reason := range []string{"first cause", "second cause", "third cause"} {
		lpRetroTask(t, env, domain.Task{Project: "beta", Title: fmt.Sprint("c", i), Status: domain.TaskBlocked,
			BlockedReason: reason, BlockedAt: pbAt(env.now.Add(-time.Duration(i+1) * time.Hour))})
	}
	seen := map[string]bool{}
	all, err := env.e.Problems(env.ctx)
	require.NoError(t, err)
	for _, p := range all {
		assert.False(t, seen[p.Key], "duplicate key %q", p.Key)
		seen[p.Key] = true
	}
}
