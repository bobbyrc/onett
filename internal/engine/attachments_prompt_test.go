package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

func TestWithAttachmentNote(t *testing.T) {
	assert.Equal(t, "hi", withAttachmentNote("hi", nil))
	a := []domain.Attachment{{Name: "n.txt", MIME: "text/plain", Size: 5, Path: "/data/x/n.txt"}}
	got := withAttachmentNote("hi", a)
	assert.True(t, strings.HasPrefix(got, "hi\n\n"))
	assert.Contains(t, got, "/data/x/n.txt")
	assert.Contains(t, withAttachmentNote("", a), "n.txt (text/plain, 5 bytes)")
}

func TestSendChatWithPutsAttachmentPathInPrompt(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var prompt string
	h := hfake.NewSteerable(func(ctx context.Context, sp harness.Spec, _ <-chan string, _ func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		prompt = sp.Prompt
		mu.Unlock()
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)
	e.d.Attachments = attach.New(r.st, t.TempDir(), func() int64 { return 1 << 20 })
	a, err := e.d.Attachments.Save(ctx, convID, "notes.txt", strings.NewReader("hello"))
	require.NoError(t, err)
	got, err := e.d.Attachments.Get(ctx, a.ID)
	require.NoError(t, err)

	_, err = e.SendChatWith(ctx, convID, "look", []string{a.ID})
	require.NoError(t, err)
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return prompt != "" }, 5e9, 10e6)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, prompt, got.Path)
	assert.Contains(t, prompt, "look")
}
