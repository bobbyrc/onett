package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeartbeatKeepsFailedSelfUpdateUntilVersionChanges(t *testing.T) {
	h := hbMemNew(t, "")
	h.e.d.Version = "v0.1-3-g0123456"
	const ref = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	h.e.selfUpdateFailed(h.ctx, "demo", ref, filepath.Join(t.TempDir(), selfUpdateLogFile), "exited with an error: exit status 1")
	const key = "self_update_failure:bbbbbbb:-"
	for i := 0; i < 6; i++ { // 90 minutes: far past the one digest the event used to be in
		h.tick()
		require.Contains(t, h.memory().Items, key, "tick %d", i+1)
	}
	require.NotEmpty(t, h.rec.digests)
	assert.True(t, hbHas(h.rec.digests[0], hbSelfUpdateFailure, "bbbbbbb"))
	var summary string
	for _, it := range h.rec.digests[0].Items {
		if it.Kind == hbSelfUpdateFailure {
			summary = it.Summary
		}
	}
	assert.Contains(t, summary, "self-update to bbbbbbb failed")
	assert.Contains(t, summary, "exit status 1")

	h.e.d.Version = "v0.1-4-gccccccc" // a later update succeeded
	h.tick()
	assert.NotContains(t, h.memory().Items, key)
}

func TestRolledBackSelfUpdateIsRecorded(t *testing.T) {
	s := suNew(t)
	const ref = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, os.WriteFile(filepath.Join(s.dir, selfUpdateResultFile), []byte("old=x\nnew=y\nref="+ref+"\nstatus=rolled_back\n"), 0o600))
	s.e.reportSelfUpdate(s.ctx)
	f, ok, err := s.e.openSelfUpdateFailure(s.ctx, "demo")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, ref, f.Ref)
	assert.Equal(t, s.e.d.Version, f.Version)
	assert.Contains(t, f.Message, "rolled back")
}

func TestSelfUpdateLogHasTimestampedLines(t *testing.T) {
	s := suNew(t)
	const ref = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const stamp = "2026-10-27T12:00:00Z onett: " // the test clock, RFC 3339 in UTC
	logPath := filepath.Join(s.dir, selfUpdateLogFile)
	readLog := func() string {
		b, err := os.ReadFile(logPath)
		require.NoError(t, err)
		return string(b)
	}

	s.run(loopSelfUpdate)
	require.Equal(t, 1, s.started())
	assert.Contains(t, readLog(), stamp+"starting update to "+ref+" (running "+s.e.d.Version+")\n")

	s.waitC <- os.ErrInvalid // the updater exits with an error
	deadline := time.Now().Add(5 * time.Second)
	for s.e.selfUpd.failures(ref) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("failure not counted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	assert.Contains(t, readLog(), stamp+"update to "+ref+" failed: exited with an error: invalid argument\n")

	require.NoError(t, os.WriteFile(filepath.Join(s.dir, selfUpdateResultFile), []byte("old=v0.1-1-g1111111\nnew="+s.e.d.Version+"\nref="+suRef+"\nstatus=ok\n"), 0o600))
	s.e.reportSelfUpdate(s.ctx)
	assert.Contains(t, readLog(), stamp+"update ok: now running "+s.e.d.Version+" (was v0.1-1-g1111111)\n")

	require.NoError(t, os.WriteFile(filepath.Join(s.dir, selfUpdateResultFile), []byte("old=x\nnew=y\nref="+ref+"\nstatus=rolled_back\n"), 0o600))
	s.e.reportSelfUpdate(s.ctx)
	assert.Contains(t, readLog(), stamp+"self-update to y did not come up healthy; rolled back to x\n")
}
