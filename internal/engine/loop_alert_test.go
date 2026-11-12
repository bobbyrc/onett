package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// laNotifier records the notifications the engine sends.
type laNotifier struct{ notes []notify.Notification }

func (n *laNotifier) Notify(_ context.Context, note notify.Notification) error {
	n.notes = append(n.notes, note)
	return nil
}

// laEnv is a loop test engine with a recording notifier; prod_watch is the loop that fails.
type laEnv struct {
	*lpEnv
	rec   *laNotifier
	start time.Time
}

func laNew(t *testing.T, projectExtra string) *laEnv {
	t.Helper()
	env := lpNewEngine(t, lpDoc(t, lpPath(t), projectExtra, ""))
	rec := &laNotifier{}
	env.e.d.Notifier = rec
	return &laEnv{lpEnv: env, rec: rec, start: env.now}
}

// fail runs prod_watch at start+after and makes it fail with text.
func (a *laEnv) fail(after time.Duration, text string) {
	a.t.Helper()
	a.now = a.start.Add(after)
	a.obs.err = errors.New(text)
	if err := a.e.RunLoop(a.ctx, loopProdWatch, "demo"); err == nil {
		a.t.Fatal("RunLoop returned nil, want the observer error")
	}
}

// ok runs prod_watch at start+after and it succeeds.
func (a *laEnv) ok(after time.Duration) {
	a.t.Helper()
	a.now = a.start.Add(after)
	a.obs.err = nil
	a.run(loopProdWatch)
}

func TestLoopAlertSlowLoopNotifiesOnce(t *testing.T) {
	a := laNew(t, "")
	a.fail(0, "sentry is down")
	a.fail(6*time.Minute, "sentry is down")
	assert.Empty(t, a.rec.notes)

	a.fail(12*time.Minute, "sentry is down")
	require.Len(t, a.rec.notes, 1)
	n := a.rec.notes[0]
	assert.Equal(t, notify.PriorityHigh, n.Priority)
	assert.Equal(t, "Loop prod_watch keeps failing (demo)", n.Title)
	assert.Contains(t, n.Body, "Failed 3 times in a row since 2026-10-27 12:00 UTC")
	assert.Contains(t, n.Body, "sentry is down")
	assert.True(t, strings.HasSuffix(n.URL, "/loops"), "URL = %q", n.URL)

	a.fail(18*time.Minute, "sentry is down")
	a.fail(24*time.Minute, "sentry is down")
	a.fail(30*time.Minute, "sentry is down")
	assert.Len(t, a.rec.notes, 1)
}

func TestLoopAlertQuickFailuresDoNotNotify(t *testing.T) {
	a := laNew(t, "")
	a.fail(0, "sentry is down")
	a.fail(30*time.Second, "sentry is down")
	a.fail(60*time.Second, "sentry is down")
	assert.Empty(t, a.rec.notes)

	a.fail(10*time.Minute, "sentry is down")
	require.Len(t, a.rec.notes, 1)
	assert.Contains(t, a.rec.notes[0].Body, "Failed 4 times in a row")
}

func TestLoopAlertNeedsThreeFailures(t *testing.T) {
	a := laNew(t, "")
	a.fail(0, "sentry is down")
	a.fail(20*time.Minute, "sentry is down")
	assert.Empty(t, a.rec.notes)
}

func TestLoopAlertRecoveryStartsANewStreak(t *testing.T) {
	a := laNew(t, "")
	a.fail(0, "sentry is down")
	a.fail(6*time.Minute, "sentry is down")
	a.fail(12*time.Minute, "sentry is down")
	require.Len(t, a.rec.notes, 1)

	a.ok(18 * time.Minute)
	a.fail(20*time.Minute, "sentry is down")
	a.fail(26*time.Minute, "sentry is down")
	assert.Len(t, a.rec.notes, 1)

	a.fail(32*time.Minute, "sentry is down")
	require.Len(t, a.rec.notes, 2)
	assert.Contains(t, a.rec.notes[1].Body, "Failed 3 times in a row since 2026-10-27 12:20 UTC")
}

func TestLoopAlertRepeatsDailyWhileFailing(t *testing.T) {
	a := laNew(t, "")
	a.fail(0, "sentry is down")
	a.fail(6*time.Minute, "sentry is down")
	a.fail(12*time.Minute, "sentry is down")
	require.Len(t, a.rec.notes, 1)

	a.fail(23*time.Hour, "sentry is down")
	assert.Len(t, a.rec.notes, 1)

	a.fail(24*time.Hour+12*time.Minute, "sentry is down")
	assert.Len(t, a.rec.notes, 2)

	a.fail(25*time.Hour, "sentry is down")
	assert.Len(t, a.rec.notes, 2)
}

func TestLoopAlertRedactsSecrets(t *testing.T) {
	const secret = "sntrys_secret_0123456"
	a := laNew(t, "    env:\n      SENTRY_AUTH_TOKEN: "+secret+"\n")
	text := "request with token " + secret + " refused"
	a.fail(0, text)
	a.fail(6*time.Minute, text)
	a.fail(12*time.Minute, text)

	require.Len(t, a.rec.notes, 1)
	n := a.rec.notes[0]
	assert.NotContains(t, n.Title, secret)
	assert.NotContains(t, n.Body, secret)
	assert.Contains(t, n.Body, "***")
}

func TestLoopAlertStreakIsStored(t *testing.T) {
	a := laNew(t, "")
	a.fail(0, "sentry is down")
	a.fail(6*time.Minute, "sentry is down")

	st, err := a.st.GetLoopState(a.ctx, "loop_alert:prod_watch", "demo")
	require.NoError(t, err)
	var s loopStreak
	require.NoError(t, json.Unmarshal([]byte(st.Cursor), &s))
	assert.Equal(t, 2, s.Fails)
	assert.True(t, s.Since.Equal(a.start), "Since = %v, want %v", s.Since, a.start)

	a.ok(12 * time.Minute)
	st, err = a.st.GetLoopState(a.ctx, "loop_alert:prod_watch", "demo")
	require.NoError(t, err)
	assert.Equal(t, "", st.Cursor)
}
