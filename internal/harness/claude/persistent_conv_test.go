package claude

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

func newStubRunner(t *testing.T) *runner {
	t.Helper()
	script, err := filepath.Abs("testdata/persistent.sh")
	require.NoError(t, err)
	return &runner{cfg: harness.Config{Command: []string{script}}}
}

var pidRe = regexp.MustCompile(`pid=(\d+) n=(\d+)`)

func TestTwoTurnsOfOneConversationShareOneProcess(t *testing.T) {
	r := newStubRunner(t)
	conv := t.Name()
	t.Cleanup(harness.StopConversations)

	// A different workdir for each call would change the fingerprint, so pin it.
	dir := t.TempDir()
	turn := func(prompt string) harness.Result {
		sess, err := r.Start(context.Background(), harness.Spec{
			Workdir: dir, Prompt: prompt, ConversationID: conv, KeepAlive: time.Minute,
			Env: map[string]string{"ONETT_RUN_ID": prompt}, // differs per turn, must not matter
		})
		require.NoError(t, err)
		for range sess.Events() {
		}
		res, err := sess.Wait()
		require.NoError(t, err)
		return res
	}
	first, second := turn("one"), turn("two")

	m1, m2 := pidRe.FindStringSubmatch(first.FinalText), pidRe.FindStringSubmatch(second.FinalText)
	require.NotNil(t, m1, first.FinalText)
	require.NotNil(t, m2, second.FinalText)
	assert.Equal(t, m1[1], m2[1], "same process answered both turns")
	assert.Equal(t, "1", m1[2])
	assert.Equal(t, "2", m2[2], "the second message was written to the same process")
	assert.Contains(t, second.FinalText, "text=two")

	// A turn's result describes that turn alone.
	assert.InDelta(t, 0.01, first.CostUSD, 1e-9)
	assert.InDelta(t, 0.01, second.CostUSD, 1e-9, "cost is the difference of the cumulative total")
	assert.Equal(t, int64(1+1), first.InputTokens)
	assert.Equal(t, int64(1), first.CachedInputTokens)
	assert.Equal(t, int64(1+2), second.InputTokens)
	assert.Equal(t, int64(2), second.CachedInputTokens)
	assert.Equal(t, "s-conv", second.SessionID)
}

func TestIdleTimeoutStopsTheProcessAndTheNextTurnResumes(t *testing.T) {
	r := newStubRunner(t)
	conv := t.Name()
	t.Cleanup(harness.StopConversations)
	dir := t.TempDir()
	turn := func(prompt, resume string) harness.Result {
		sess, err := r.Start(context.Background(), harness.Spec{
			Workdir: dir, Prompt: prompt, ConversationID: conv, KeepAlive: 150 * time.Millisecond,
			ResumeSessionID: resume,
		})
		require.NoError(t, err)
		for range sess.Events() {
		}
		res, _ := sess.Wait()
		return res
	}
	first := turn("one", "")
	assert.NotContains(t, first.FinalText, "--resume")

	time.Sleep(600 * time.Millisecond) // past the idle timeout
	second := turn("two", first.SessionID)

	m1, m2 := pidRe.FindStringSubmatch(first.FinalText), pidRe.FindStringSubmatch(second.FinalText)
	require.NotNil(t, m1)
	require.NotNil(t, m2)
	assert.NotEqual(t, m1[1], m2[1], "a new process answered after the idle timeout")
	assert.Equal(t, "1", m2[2])
	assert.Contains(t, second.FinalText, "--resume s-conv", "the new process resumes the session")
}

func TestProcessDyingMidTurnFailsThatTurnAndTheNextOneStartsAgain(t *testing.T) {
	r := newStubRunner(t)
	conv := t.Name()
	t.Cleanup(harness.StopConversations)
	dir := t.TempDir()
	turn := func(prompt string) harness.Result {
		sess, err := r.Start(context.Background(), harness.Spec{
			Workdir: dir, Prompt: prompt, ConversationID: conv, KeepAlive: time.Minute,
		})
		require.NoError(t, err)
		for range sess.Events() {
		}
		res, err := sess.Wait()
		require.NoError(t, err)
		return res
	}
	ok := turn("one")
	dead := turn("die now")
	assert.True(t, dead.IsError)
	assert.Equal(t, 3, dead.ExitCode)

	again := turn("three")
	assert.False(t, again.IsError)
	assert.NotEqual(t, pidRe.FindStringSubmatch(ok.FinalText)[1], pidRe.FindStringSubmatch(again.FinalText)[1])
}

func TestCancelKillsTheConversationProcess(t *testing.T) {
	r := newStubRunner(t)
	conv := t.Name()
	t.Cleanup(harness.StopConversations)
	dir := t.TempDir()
	start := func(prompt string) harness.Session {
		sess, err := r.Start(context.Background(), harness.Spec{
			Workdir: dir, Prompt: prompt, ConversationID: conv, KeepAlive: time.Minute,
		})
		require.NoError(t, err)
		return sess
	}
	// Cancel at once, before the stub has answered.
	s := start("one")
	s.Cancel()
	for range s.Events() {
	}
	res, err := s.Wait()
	require.NoError(t, err)
	_ = res
	// The next turn must not reuse the killed process.
	s2 := start("two")
	for range s2.Events() {
	}
	res2, err := s2.Wait()
	require.NoError(t, err)
	assert.False(t, res2.IsError)
	assert.Contains(t, res2.FinalText, "n=1 ", "a fresh process answered")
}
