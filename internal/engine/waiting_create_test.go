package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
)

func TestCreateTaskWaitsOnPendingTasks(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	pending := wtParent(env, domain.TaskReview, "")
	landed := wtParent(env, domain.TaskMerged, "")

	got, err := env.e.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindChore, Title: "next step",
		WaitingOn: []string{pending.ID, landed.ID, pending.ID, " "},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{pending.ID}, got.WaitingOn, "landed and repeated entries are dropped")
}

func TestCreateTaskRejectsBadWaitingOn(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	failed := wtParent(env, domain.TaskFailed, "")
	for name, ids := range map[string][]string{"unknown": {"nope"}, "will not merge": {failed.ID}} {
		_, err := env.e.CreateTask(env.ctx, domain.Task{
			Project: "demo", Kind: domain.KindChore, Title: "x", WaitingOn: ids,
		})
		assert.True(t, errors.Is(err, api.ErrInvalid), "%s: %v", name, err)
	}
}
