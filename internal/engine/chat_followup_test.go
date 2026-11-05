package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// cfRig is an engine with a test clock, an orchestrator that records its prompts, and one
// conversation.
type cfRig struct {
	r       *ctRig
	e       *Engine
	conv    string
	now     *time.Time
	mu      sync.Mutex
	prompts []string
}

func newCfRig(t *testing.T) *cfRig {
	t.Helper()
	cf := &cfRig{}
	h := hfake.New(func(_ context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		cf.mu.Lock()
		cf.prompts = append(cf.prompts, spec.Prompt)
		cf.mu.Unlock()
		return harness.Result{SessionID: "s", FinalText: "noted"}, nil
	})
	r, e, conv := cqRig(t, h)
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cf.r, cf.e, cf.conv, cf.now = r, e, conv, &start
	e.d.Now = func() time.Time { return *cf.now }
	return cf
}

func (cf *cfRig) advance(d time.Duration) { *cf.now = cf.now.Add(d) }

func (cf *cfRig) promptCount() int {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	return len(cf.prompts)
}

// task creates a task from source in status.
func (cf *cfRig) task(source string, status domain.TaskStatus) domain.Task {
	cf.r.t.Helper()
	t, err := cf.r.st.CreateTask(context.Background(), domain.Task{
		Project: "demo", Kind: domain.KindFeature, Title: "do " + string(status), Source: source,
		Status: domain.TaskQueued, MaxAttempts: 2,
	})
	require.NoError(cf.r.t, err)
	if status != domain.TaskQueued {
		cf.set(t.ID, status)
	}
	return t
}

func (cf *cfRig) set(id string, status domain.TaskStatus) {
	ctSetTask(cf.r.t, cf.r, id, func(t *domain.Task) { t.Status = status })
}

func (cf *cfRig) tick() { cf.e.chatFollowupTick(context.Background()) }

// settle runs the first (seeding) tick, then moves a new chat task to status and ticks again.
func (cf *cfRig) settle(status domain.TaskStatus) domain.Task {
	cf.tick() // seed
	t := cf.task(domain.SourceChatPrefix+cf.conv, domain.TaskQueued)
	cf.tick() // sees it queued
	cf.set(t.ID, status)
	cf.tick() // sees it settle
	return t
}

func (cf *cfRig) waitIdle() {
	cf.r.t.Helper()
	require.Eventually(cf.r.t, func() bool {
		c, err := cf.r.st.GetConversation(context.Background(), cf.conv)
		return err == nil && !c.Busy
	}, 10*time.Second, 5*time.Millisecond)
	cf.e.wg.Wait()
}

func TestChatFollowupOneTurnAfterDebounce(t *testing.T) {
	cf := newCfRig(t)
	settled := cf.settle(domain.TaskDone)

	cf.advance(chatFollowupDebounce - time.Second)
	cf.tick()
	assert.Equal(t, 0, cf.promptCount(), "before the debounce")

	cf.advance(time.Second)
	cf.tick()
	cf.waitIdle()
	require.Equal(t, 1, cf.promptCount())
	assert.Contains(t, cf.prompts[0], settled.ID)
	assert.Contains(t, cf.prompts[0], "now done")

	cf.advance(time.Hour)
	cf.tick()
	cf.waitIdle()
	assert.Equal(t, 1, cf.promptCount(), "reported once")

	msgs, err := cf.r.st.ListChatMessages(context.Background(), cf.conv)
	require.NoError(t, err)
	require.NotEmpty(t, msgs)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Equal(t, domain.ChatKindInternal, msgs[0].Kind, "the prompt is hidden from the owner")
	for _, m := range msgs {
		assert.NotEqual(t, "user", m.Role)
	}
}

func TestChatFollowupBatchesSettles(t *testing.T) {
	cf := newCfRig(t)
	a := cf.settle(domain.TaskDone)
	cf.advance(30 * time.Second)
	b := cf.task(domain.SourceChatPrefix+cf.conv, domain.TaskQueued)
	cf.tick()
	cf.set(b.ID, domain.TaskBlocked)
	cf.tick()

	cf.advance(chatFollowupDebounce)
	cf.tick()
	cf.waitIdle()
	require.Equal(t, 1, cf.promptCount())
	assert.Contains(t, cf.prompts[0], a.ID)
	assert.Contains(t, cf.prompts[0], b.ID)
}

func TestChatFollowupSkipsBusyAndClosedConversations(t *testing.T) {
	cf := newCfRig(t)
	cf.settle(domain.TaskDone)
	ctx := context.Background()

	_, err := cf.r.st.UpdateConversation(ctx, cf.conv, func(c *domain.Conversation) error { c.Busy = true; return nil })
	require.NoError(t, err)
	cf.advance(chatFollowupDebounce)
	cf.tick()
	assert.Equal(t, 0, cf.promptCount(), "busy")

	_, err = cf.r.st.UpdateConversation(ctx, cf.conv, func(c *domain.Conversation) error {
		c.Busy = false
		now := cf.e.d.Now()
		c.ClosedAt = &now
		return nil
	})
	require.NoError(t, err)
	cf.tick()
	assert.Equal(t, 0, cf.promptCount(), "closed")
	_, still := cf.e.followups.pending[cf.conv]
	assert.False(t, still, "closed conversation drops its pending items")
}

func TestChatFollowupWaitsWhilePaused(t *testing.T) {
	cf := newCfRig(t)
	cf.settle(domain.TaskDone)
	require.NoError(t, cf.e.SetPaused(context.Background(), true))
	cf.advance(chatFollowupDebounce)
	cf.tick()
	assert.Equal(t, 0, cf.promptCount())

	require.NoError(t, cf.e.SetPaused(context.Background(), false))
	cf.tick()
	cf.waitIdle()
	assert.Equal(t, 1, cf.promptCount(), "wakes once resumed")
}

func TestChatFollowupIgnoresNonChatTasks(t *testing.T) {
	cf := newCfRig(t)
	cf.tick()
	for _, src := range []string{"api", "followup", "conversation", "chat:"} {
		tk := cf.task(src, domain.TaskQueued)
		cf.tick()
		cf.set(tk.ID, domain.TaskDone)
	}
	cf.tick()
	cf.advance(time.Hour)
	cf.tick()
	assert.Equal(t, 0, cf.promptCount())
}

func TestChatFollowupSkipsTasksSettledBeforeStartup(t *testing.T) {
	cf := newCfRig(t)
	cf.task(domain.SourceChatPrefix+cf.conv, domain.TaskDone)
	cf.tick() // first tick seeds
	cf.advance(time.Hour)
	cf.tick()
	assert.Equal(t, 0, cf.promptCount())
}

func TestChatFollowupRateLimitsTurns(t *testing.T) {
	cf := newCfRig(t)
	first := cf.settle(domain.TaskBlocked)
	cf.advance(chatFollowupDebounce)
	cf.tick()
	cf.waitIdle()
	require.Equal(t, 1, cf.promptCount())

	// The retry settles again right away: it waits out the window before the next turn.
	cf.set(first.ID, domain.TaskQueued)
	cf.tick()
	cf.set(first.ID, domain.TaskDone)
	cf.tick()
	cf.advance(chatFollowupDebounce - time.Second)
	cf.tick()
	assert.Equal(t, 1, cf.promptCount())
	cf.advance(time.Second)
	cf.tick()
	cf.waitIdle()
	assert.Equal(t, 2, cf.promptCount(), "a later settle of the same task wakes again")
}
