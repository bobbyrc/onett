package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/store"
)

func TestSendChatWithStoresAttachments(t *testing.T) {
	ctx := context.Background()
	h := hfake.NewSteerable(func(ctx context.Context, _ harness.Spec, _ <-chan string, _ func(harness.Event)) (harness.Result, error) {
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)
	e.d.Attachments = attach.New(r.st, t.TempDir(), func() int64 { return 1 << 20 })
	a, err := e.d.Attachments.Save(ctx, convID, "notes.txt", strings.NewReader("hello"))
	require.NoError(t, err)

	// Text may be empty when there is an attachment.
	msg, err := e.SendChatWith(ctx, convID, "", []string{a.ID})
	require.NoError(t, err)
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, a.ID, msg.Attachments[0].ID)
	assert.Equal(t, "notes.txt", msg.Attachments[0].Name)

	msgs, err := r.st.ListChatMessages(ctx, convID)
	require.NoError(t, err)
	var found bool
	for _, m := range msgs {
		if m.ID == msg.ID {
			found = true
			require.Len(t, m.Attachments, 1)
			assert.Equal(t, "text/plain", m.Attachments[0].MIME)
		}
	}
	assert.True(t, found)

	// Empty with nothing attached, and an attachment from another conversation, are refused.
	_, err = e.SendChatWith(ctx, convID, "  ", nil)
	assert.ErrorIs(t, err, api.ErrInvalid)
	other, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)
	_, err = e.SendChatWith(ctx, other.ID, "x", []string{a.ID})
	assert.ErrorIs(t, err, api.ErrInvalid)
}

func TestHousekeepPrunesOldAttachments(t *testing.T) {
	env := lpNewEngine(t, "chat:\n  attachments:\n    retention: 48h\n")
	dir := filepath.Join(t.TempDir(), "attachments")
	env.e.d.Attachments = attach.New(env.st, dir, func() int64 { return 1 << 20 })

	put := func(conv, name string, age time.Duration) domain.Attachment {
		p := filepath.Join(dir, conv, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
		a, err := env.st.CreateAttachment(env.ctx, domain.Attachment{
			ID: name, ConversationID: conv, Name: name, MIME: "text/plain", Size: 1,
			CreatedAt: env.now.Add(-age), Path: p,
		})
		require.NoError(t, err)
		return a
	}
	old := put("c1", "old", 72*time.Hour)
	fresh := put("c1", "fresh", 24*time.Hour)
	oldAlone := put("c2", "old2", 100*time.Hour)

	env.e.housekeep(env.ctx)

	for _, a := range []domain.Attachment{old, oldAlone} {
		_, err := os.Stat(a.Path)
		assert.True(t, os.IsNotExist(err), a.Name)
		_, err = env.st.GetAttachment(env.ctx, a.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	}
	_, err := os.Stat(fresh.Path)
	assert.NoError(t, err)
	_, err = env.st.GetAttachment(env.ctx, fresh.ID)
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "c2"))
	assert.True(t, os.IsNotExist(err), "empty conversation directory removed")
}
