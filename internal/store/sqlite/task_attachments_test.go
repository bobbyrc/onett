package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

func TestTaskAttachmentsRoundTripAndExpire(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a, err := s.CreateAttachment(ctx, domain.Attachment{ConversationID: "c1", Name: "n.txt", MIME: "text/plain", Size: 3, Path: "/d/c1/x-n.txt"})
	require.NoError(t, err)

	task, err := s.CreateTask(ctx, domain.Task{Project: "p", Title: "t", Attachments: []domain.Attachment{{ID: a.ID}}})
	require.NoError(t, err)
	require.Len(t, task.Attachments, 1)
	assert.Equal(t, "/d/c1/x-n.txt", task.Attachments[0].Path)

	got, err := s.GetTask(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, got.Attachments, 1)
	assert.Equal(t, "n.txt", got.Attachments[0].Name)
	assert.False(t, got.Attachments[0].Expired)

	// An update keeps them; listing and claiming carry them too.
	up, err := s.UpdateTask(ctx, task.ID, func(x *domain.Task) error { x.Status = domain.TaskQueued; return nil })
	require.NoError(t, err)
	assert.Len(t, up.Attachments, 1)
	list, err := s.ListTasks(ctx, store.TaskFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Len(t, list[0].Attachments, 1)
	claimed, err := s.ClaimNextTask(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, claimed.Attachments, 1)

	// Retention deletes the record; the task still lists the file, as expired.
	require.NoError(t, s.DeleteAttachment(ctx, a.ID))
	got, err = s.GetTask(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, got.Attachments, 1)
	assert.True(t, got.Attachments[0].Expired)
	assert.Equal(t, "n.txt", got.Attachments[0].Name)
	assert.Empty(t, got.Attachments[0].Path)

	// An unknown attachment fails the create and leaves no task.
	_, err = s.CreateTask(ctx, domain.Task{Project: "p", Title: "u", Attachments: []domain.Attachment{{ID: "nope"}}})
	assert.ErrorIs(t, err, store.ErrNotFound)
	list, err = s.ListTasks(ctx, store.TaskFilter{})
	require.NoError(t, err)
	assert.Len(t, list, 1)
}
