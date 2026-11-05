package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

func TestClaimNextTaskSkipsHeldProjectRoles(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, err := s.CreateTask(ctx, domain.Task{Project: "p", Role: "implementer", Status: domain.TaskQueued, Priority: 9})
	require.NoError(t, err)
	other, err := s.CreateTask(ctx, domain.Task{Project: "p", Role: "reviewer", Status: domain.TaskQueued, Priority: 1})
	require.NoError(t, err)

	c, err := s.ClaimNextTask(ctx, nil, store.ProjectRole{Project: "p", Role: "implementer"})
	require.NoError(t, err)
	assert.Equal(t, other.ID, c.ID, "the held role is skipped, the other role is claimed")

	_, err = s.ClaimNextTask(ctx, nil, store.ProjectRole{Project: "p", Role: "implementer"})
	assert.True(t, errors.Is(err, store.ErrNotFound))

	c, err = s.ClaimNextTask(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, "implementer", c.Role, "with no hold the task is claimable again")
}
