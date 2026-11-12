package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/notify"
)

func TestParseHeartbeatReplies(t *testing.T) {
	ids := map[string]bool{"t1": true, "run-2": true}
	for _, tc := range []struct {
		name, in string
		want     []hbReplyLine
	}{
		{"plain", "t1: retried it", []hbReplyLine{{"t1", "retried it"}}},
		{"backticks", "`t1`: left alone: waiting on CI", []hbReplyLine{{"t1", "left alone: waiting on CI"}}},
		{"bullet", "- t1: cancelled", []hbReplyLine{{"t1", "cancelled"}}},
		{"star bullet and backticks", "* `run-2`: escalate: denied again", []hbReplyLine{{"run-2", "escalate: denied again"}}},
		{"numbered", "1. t1: retried", []hbReplyLine{{"t1", "retried"}}},
		{"bold id", "**t1**: retried", []hbReplyLine{{"t1", "retried"}}},
		{"unknown id", "other: retried", nil},
		{"prose", "I looked at everything.\nNothing to add: really", nil},
		{"empty text", "t1:", nil},
		{"two lines", "Summary below\n- t1: a\n- run-2: b", []hbReplyLine{{"t1", "a"}, {"run-2", "b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseHeartbeatReplies(tc.in, ids))
		})
	}
	assert.True(t, hbReplyLine{"t1", "Escalate: why"}.isEscalation())
	assert.False(t, hbReplyLine{"t1", "left alone: escalate later"}.isEscalation())
}

func TestHeartbeatStateMigratesOldCursor(t *testing.T) {
	first := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	old, err := json.Marshal(map[string]any{
		"seen":     map[string]time.Time{"blocked_task:abc:1234abcd": first, "loop_error:issue:intake:-": first},
		"built_at": first,
	})
	require.NoError(t, err)
	s := loadHeartbeatState(string(old))
	require.Len(t, s.Items, 2)
	assert.Equal(t, hbItemMemory{EntityID: "abc", FirstSeen: first, Heartbeats: 1}, s.Items["blocked_task:abc:1234abcd"])
	assert.Equal(t, "issue:intake", s.Items["loop_error:issue:intake:-"].EntityID)
	assert.Nil(t, s.Seen)
	assert.Empty(t, loadHeartbeatState("not json").Items)
}

// hbMemEnv is a heartbeat test engine with a recording notifier and an Operations conversation.
type hbMemEnv struct {
	*lpEnv
	rec   *hbRecorder
	notes []notify.Notification
	conv  domain.Conversation
}

type hbNotifier struct{ h *hbMemEnv }

func (n hbNotifier) Notify(_ context.Context, note notify.Notification) error {
	n.h.notes = append(n.h.notes, note)
	return nil
}

func hbMemNew(t *testing.T, topExtra string) *hbMemEnv {
	t.Helper()
	env, rec := hbNewEnv(t, topExtra)
	h := &hbMemEnv{lpEnv: env, rec: rec}
	env.e.d.Notifier = hbNotifier{h}
	conv, err := env.st.CreateConversation(env.ctx, domain.Conversation{Title: "Operations", Project: "demo", Kind: domain.ConversationOperations})
	require.NoError(t, err)
	h.conv = conv
	return h
}

// tick advances the clock and runs the loop.
func (h *hbMemEnv) tick() {
	h.t.Helper()
	h.now = h.now.Add(15 * time.Minute)
	h.run(loopHeartbeat)
}

func (h *hbMemEnv) reply(text string) {
	h.t.Helper()
	_, err := h.st.AppendChatMessage(h.ctx, domain.ChatMessage{ConversationID: h.conv.ID, Role: "assistant", Text: text, CreatedAt: h.now})
	require.NoError(h.t, err)
}

func (h *hbMemEnv) blocked(title string) domain.Task {
	return lpRetroTask(h.t, h.lpEnv, domain.Task{Title: title, Status: domain.TaskBlocked, BlockedReason: "stuck"})
}

func (h *hbMemEnv) memory() heartbeatState {
	h.t.Helper()
	st, err := h.st.GetLoopState(h.ctx, loopHeartbeat, "demo")
	require.NoError(h.t, err)
	return loadHeartbeatState(st.Cursor)
}

func TestHeartbeatRecordsActionsAndShowsThem(t *testing.T) {
	h := hbMemNew(t, "")
	a, b := h.blocked("A"), h.blocked("B")
	h.tick()
	h.reply("Looked at both.\n- `" + a.ID + "`: retried with a note\n- " + b.ID + ": left alone: waiting on CI\nunrelated: line")
	h.tick()

	mem := h.memory()
	var ka, kb string
	for k, m := range mem.Items {
		if m.EntityID == a.ID {
			ka = k
		} else if m.EntityID == b.ID {
			kb = k
		}
	}
	require.Len(t, mem.Items[ka].Actions, 1)
	assert.Contains(t, mem.Items[ka].Actions[0], "retried with a note")
	assert.True(t, strings.HasPrefix(mem.Items[ka].Actions[0], h.now.UTC().Add(-15*time.Minute).Format(time.RFC3339)+": "))
	require.Len(t, mem.Items[kb].Actions, 1)
	assert.Contains(t, mem.Items[kb].Actions[0], "left alone: waiting on CI")

	// A reply is read once, however many ticks follow.
	h.tick()
	assert.Len(t, h.memory().Items[ka].Actions, 1)

	// The next wake shows the memory to the orchestrator.
	c := h.blocked("C")
	h.tick()
	last := h.rec.digests[len(h.rec.digests)-1]
	prompt := heartbeatPrompt(last, h.e.Config())
	assert.Contains(t, prompt, "previously: ")
	assert.Contains(t, prompt, "retried with a note")
	assert.Contains(t, prompt, "Do not repeat an action already listed")
	assert.True(t, hbHas(last, hbBlockedTask, c.ID))
	for _, it := range last.Items {
		if it.EntityID == a.ID {
			assert.Equal(t, 4, it.Heartbeats)
		}
		if it.EntityID == c.ID {
			assert.Equal(t, 1, it.Heartbeats)
		}
	}
}

func TestHeartbeatKeepsLastFiveActions(t *testing.T) {
	h := hbMemNew(t, "loops:\n  heartbeat:\n    escalate_after: 100\n")
	a := h.blocked("A")
	h.tick()
	for i := 0; i < 7; i++ {
		h.reply(a.ID + ": try " + string(rune('a'+i)))
		h.tick()
	}
	var acts []string
	for _, m := range h.memory().Items {
		acts = m.Actions
	}
	require.Len(t, acts, hbKeepActions)
	assert.Contains(t, acts[0], "try c")
	assert.Contains(t, acts[4], "try g")
}

func TestHeartbeatForgetsResolvedItems(t *testing.T) {
	h := hbMemNew(t, "")
	a := h.blocked("A")
	h.tick()
	h.reply(a.ID + ": retried")
	h.tick()
	_, err := h.st.UpdateTask(h.ctx, a.ID, func(x *domain.Task) error { x.Status = domain.TaskMerged; return nil })
	require.NoError(t, err)
	h.tick()
	assert.Empty(t, h.memory().Items)

	// The same task blocked again for the same reason starts from nothing.
	_, err = h.st.UpdateTask(h.ctx, a.ID, func(x *domain.Task) error { x.Status = domain.TaskBlocked; return nil })
	require.NoError(t, err)
	h.tick()
	for _, m := range h.memory().Items {
		assert.Empty(t, m.Actions)
		assert.Equal(t, 1, m.Heartbeats)
	}
}

func TestHeartbeatEscalatesOnceAfterThreeHeartbeats(t *testing.T) {
	h := hbMemNew(t, "")
	h.e.Config().Server.PublicURL = "https://onett.example"
	a := h.blocked("A")
	h.tick()
	h.tick()
	assert.Empty(t, h.notes)
	h.reply(a.ID + ": retried it")
	h.tick() // third heartbeat, digest unchanged: no wake, still escalates
	assert.Len(t, h.rec.digests, 1)
	require.Len(t, h.notes, 1)
	n := h.notes[0]
	assert.Equal(t, notify.PriorityHigh, n.Priority)
	assert.Contains(t, n.Title, a.ID)
	assert.Contains(t, n.Body, "retried it")
	assert.Contains(t, n.Body, "30m0s")
	assert.Equal(t, "https://onett.example/chat/"+h.conv.ID, n.URL)

	h.tick()
	h.tick()
	assert.Len(t, h.notes, 1, "never twice for the same key")
	for _, m := range h.memory().Items {
		assert.True(t, m.Escalated)
	}
}

func TestHeartbeatEscalateAfterIsConfigurable(t *testing.T) {
	h := hbMemNew(t, "loops:\n  heartbeat:\n    escalate_after: 2\n")
	h.blocked("A")
	h.tick()
	assert.Empty(t, h.notes)
	h.tick()
	assert.Len(t, h.notes, 1)
}

func TestHeartbeatQuietTicksDoNotCount(t *testing.T) {
	h := hbMemNew(t, "loops:\n  heartbeat:\n    quiet_hours: \"22:00-07:00\"\n")
	h.blocked("A")
	at := func(hour int) { h.now = time.Date(2026, 10, 27, hour, 0, 0, 0, time.Local); h.run(loopHeartbeat) }
	at(20)
	at(21)
	for _, hour := range []int{22, 23} {
		at(hour)
	}
	assert.Empty(t, h.notes, "two counted heartbeats so far")
	at(8) // the third counted heartbeat
	assert.Len(t, h.notes, 1)
}

func TestHeartbeatEscalateLineEscalatesAtOnce(t *testing.T) {
	h := hbMemNew(t, "")
	a := h.blocked("A")
	h.tick()
	h.reply(a.ID + ": escalate: the same permission was denied three times")
	h.tick()
	require.Len(t, h.notes, 1)
	assert.Contains(t, h.notes[0].Body, "denied three times")
	h.reply(a.ID + ": escalate: still denied")
	h.tick()
	h.tick()
	assert.Len(t, h.notes, 1, "only once")
}

func TestHeartbeatEscalationNotifiesAfterWakeFails(t *testing.T) {
	h := hbMemNew(t, "")
	h.blocked("A")
	h.rec.err = assert.AnError
	h.now = h.now.Add(time.Minute)
	require.Error(t, h.e.RunLoop(h.ctx, loopHeartbeat, "demo"))
	assert.Empty(t, h.notes)
	require.Len(t, h.memory().Items, 1, "a failed wake still counts the heartbeat")
	for _, m := range h.memory().Items {
		assert.Equal(t, 1, m.Heartbeats)
		assert.True(t, m.Unwoken, "still new for the next tick")
	}
}

var _ = config.Config{}
