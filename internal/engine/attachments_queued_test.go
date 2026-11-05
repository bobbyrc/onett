package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// An attachment sent while the agent is busy waits in the queue as a stored message; its path must
// survive the reload from the store so the flushed turn's prompt still lists the file.
func TestQueuedChatAttachmentPathReachesFlushedPrompt(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	var mu sync.Mutex
	var prompts []string
	h := hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		prompts = append(prompts, spec.Prompt)
		n := len(prompts)
		mu.Unlock()
		if n == 1 {
			<-release
		}
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)
	e.d.Attachments = attach.New(r.st, t.TempDir(), func() int64 { return 1 << 20 })
	a, err := e.d.Attachments.Save(ctx, convID, "notes.txt", strings.NewReader("hello"))
	require.NoError(t, err)
	saved, err := e.d.Attachments.Get(ctx, a.ID)
	require.NoError(t, err)

	_, err = e.SendChat(ctx, convID, "go")
	require.NoError(t, err)
	cqWaitSession(t, e, ctActiveRunID(t, e))

	m, err := e.SendChatWith(ctx, convID, "see file", []string{a.ID})
	require.NoError(t, err)
	require.True(t, m.Queued)

	close(release)
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(prompts) == 2 },
		10*time.Second, 10*time.Millisecond)
	ctWaitIdle(t, r, convID)

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, prompts[1], "see file")
	assert.Contains(t, prompts[1], saved.Path)
}
