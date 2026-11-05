package claude

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/runshim"
)

// shimReexec makes the test binary act as `onett run-shim`, so these tests need no built onett.
const shimReexec = "ONETT_CLAUDE_TEST_RUNSHIM"

func TestMain(m *testing.M) {
	if os.Getenv(shimReexec) == "1" {
		os.Exit(runshim.Main(os.Args[1:], os.Stderr))
	}
	os.Exit(m.Run())
}

func newShimRunner(t *testing.T, conv string) *runner {
	t.Helper()
	r := newStubRunner(t)
	dir := t.TempDir()
	r.cfg.Shim = &harness.ShimOptions{
		DataDir: dir,
		Launch: runshim.Launch{
			Command: []string{os.Args[0]},
			Env:     []string{"PATH=" + os.Getenv("PATH"), shimReexec + "=1", "GORACE=atexit_sleep_ms=0"},
		},
		Grace: 300 * time.Millisecond,
	}
	// The shim writes its final status after the daemon's last call; wait for it to exit
	// before the temp directory is removed.
	t.Cleanup(func() {
		harness.StopConversations()
		st, err := runshim.ReadStatus(runshim.RunDir(dir, harness.ConvRunID(conv)))
		if err != nil {
			return
		}
		if !st.Exited {
			_ = syscall.Kill(-st.AgentPGID, syscall.SIGKILL)
		}
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if syscall.Kill(st.ShimPID, 0) == syscall.ESRCH {
				return
			}
		}
	})
	return r
}

func shimTurn(t *testing.T, r *runner, conv, dir, prompt string) harness.Result {
	t.Helper()
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

// StopConversations lets go of a process behind a shim, as a shutdown of onett does; the next
// turn (here in the same test process, in life in the next onett) must find it again.
func TestConversationProcessSurvivesARestart(t *testing.T) {
	conv := t.Name()
	r := newShimRunner(t, conv)
	dir := t.TempDir()

	first := shimTurn(t, r, conv, dir, "one")
	harness.StopConversations() // the "restart"
	second := shimTurn(t, r, conv, dir, "two")

	m1, m2 := pidRe.FindStringSubmatch(first.FinalText), pidRe.FindStringSubmatch(second.FinalText)
	require.NotNil(t, m1, first.FinalText)
	require.NotNil(t, m2, second.FinalText)
	assert.Equal(t, m1[1], m2[1], "the same process answered after the restart")
	assert.Equal(t, "2", m2[2])
	assert.Contains(t, second.FinalText, "text=two")
	assert.InDelta(t, 0.01, second.CostUSD, 1e-9, "the new parser rebuilt the cumulative cost from the log")
	assert.Equal(t, int64(2), second.CachedInputTokens)
}

func TestChangedSettingsReplaceTheProcessAfterARestart(t *testing.T) {
	conv := t.Name()
	r := newShimRunner(t, conv)

	first := shimTurn(t, r, conv, t.TempDir(), "one")
	harness.StopConversations()
	second := shimTurn(t, r, conv, t.TempDir(), "two") // another workdir, another fingerprint

	m1, m2 := pidRe.FindStringSubmatch(first.FinalText), pidRe.FindStringSubmatch(second.FinalText)
	require.NotNil(t, m1, first.FinalText)
	require.NotNil(t, m2, second.FinalText)
	assert.NotEqual(t, m1[1], m2[1])
	assert.Equal(t, "1", m2[2])
}
