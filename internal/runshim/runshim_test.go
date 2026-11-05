package runshim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shimEnv is the agent environment in tests: just enough to find sh and coreutils.
func shimEnv() []string { return []string{"PATH=" + os.Getenv("PATH")} }

type testShim struct {
	spec   Spec
	client *Client
	done   chan struct{} // closed when Run returns; err is its result
	err    error
}

// startInProc runs the shim in this process (the agent is still a real child) and dials it.
func startInProc(t *testing.T, script string, mod func(*Spec)) *testShim {
	t.Helper()
	dataDir := t.TempDir()
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	sock, err := SocketPath(dataDir, runID)
	require.NoError(t, err)
	spec := Spec{
		RunID: runID, RunDir: RunDir(dataDir, runID), SocketPath: sock,
		Command: []string{"sh", "-c", script}, Dir: dataDir, Env: shimEnv(),
		Grace: time.Minute,
	}
	if mod != nil {
		mod(&spec)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sh := &testShim{spec: spec, done: make(chan struct{})}
	go func() { sh.err = Run(ctx, spec); close(sh.done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-sh.done:
		case <-time.After(10 * time.Second):
			t.Error("shim did not stop")
		}
	})
	sh.client = dialRetry(t, sock)
	return sh
}

func dialRetry(t *testing.T, sock string) *Client {
	t.Helper()
	var c *Client
	var err error
	require.Eventually(t, func() bool { c, err = Dial(sock); return err == nil }, 5*time.Second, 5*time.Millisecond, "dial: %v", err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// collect reads a stream to its end.
func collect(t *testing.T, s *Stream) []StreamMsg {
	t.Helper()
	var out []StreamMsg
	timeout := time.After(15 * time.Second)
	for {
		select {
		case m, ok := <-s.C:
			if !ok {
				return out
			}
			out = append(out, m)
		case <-timeout:
			t.Fatalf("stream did not end; got %d messages", len(out))
		}
	}
}

func lines(msgs []StreamMsg) []string {
	var out []string
	for _, m := range msgs {
		if m.Type == MsgLine {
			out = append(out, m.Text)
		}
	}
	return out
}

func waitExited(t *testing.T, c *Client) Status {
	t.Helper()
	var st Status
	require.Eventually(t, func() bool {
		var err error
		st, err = c.Status()
		return err == nil && st.Exited
	}, 15*time.Second, 10*time.Millisecond)
	return st
}

func TestHelloAcceptsMatchingVersion(t *testing.T) {
	sh := startInProc(t, "sleep 30", nil)
	st, err := sh.client.Status()
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, st.Version)
	assert.Equal(t, sh.spec.RunID, st.RunID)
}

func TestHelloRefusesOtherVersion(t *testing.T) {
	sh := startInProc(t, "sleep 30", nil)
	_, err := DialVersion(sh.spec.SocketPath, ProtocolVersion+1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVersionMismatch)
	assert.Contains(t, err.Error(), "version mismatch")
	assert.Contains(t, err.Error(), fmt.Sprintf("shim speaks %d", ProtocolVersion))
}

func TestReplayFromOffsets(t *testing.T) {
	sh := startInProc(t, "printf 'aa\\nb\\ncccc\\n'", nil)
	st := waitExited(t, sh.client)
	require.EqualValues(t, 3+2+5, st.LogSize)

	all, err := sh.client.Replay(0)
	require.NoError(t, err)
	msgs := collect(t, all)
	assert.Equal(t, []string{"aa", "b", "cccc"}, lines(msgs))
	assert.Equal(t, []int64{0, 3, 5}, []int64{msgs[0].Offset, msgs[1].Offset, msgs[2].Offset})
	assert.Equal(t, []int64{3, 5, 10}, []int64{msgs[0].Next, msgs[1].Next, msgs[2].Next})
	last := msgs[len(msgs)-1]
	assert.Equal(t, MsgEOF, last.Type)
	assert.EqualValues(t, 10, last.Offset)
	assert.Zero(t, last.ExitCode)

	mid, err := sh.client.Replay(3)
	require.NoError(t, err)
	assert.Equal(t, []string{"b", "cccc"}, lines(collect(t, mid)))

	end, err := sh.client.Replay(10)
	require.NoError(t, err)
	msgs = collect(t, end)
	assert.Empty(t, lines(msgs))
	require.Len(t, msgs, 1)
	assert.Equal(t, MsgEOF, msgs[0].Type)

	_, err = sh.client.Replay(1) // inside the first line
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start of a line")
	_, err = sh.client.Replay(11)
	require.Error(t, err)
}

func TestReplayHandsOverToLiveWithoutLossOrDuplicates(t *testing.T) {
	const n = 400
	sh := startInProc(t, fmt.Sprintf("i=1; while [ $i -le %d ]; do echo $i; i=$((i+1)); if [ $((i %% 20)) -eq 0 ]; then sleep 0.01; fi; done", n), nil)
	// Start replaying once some lines exist but the agent is still writing.
	require.Eventually(t, func() bool {
		st, err := sh.client.Status()
		return err == nil && st.LogSize > 0
	}, 5*time.Second, time.Millisecond)
	s, err := sh.client.Replay(0)
	require.NoError(t, err)
	got := lines(collect(t, s))
	require.Len(t, got, n)
	for i, l := range got {
		require.Equal(t, strconv.Itoa(i+1), l)
	}
}

func TestWriteAndCloseStdinReachTheAgent(t *testing.T) {
	sh := startInProc(t, `while read l; do echo "got $l"; done`, func(s *Spec) {
		s.KeepStdinOpen = true
		s.Stdin = "first\n"
	})
	s, err := sh.client.Replay(0)
	require.NoError(t, err)
	require.NoError(t, sh.client.WriteStdin([]byte("second\n")))
	require.NoError(t, sh.client.CloseStdin())
	msgs := collect(t, s)
	assert.Equal(t, []string{"got first", "got second"}, lines(msgs))
	assert.Equal(t, MsgEOF, msgs[len(msgs)-1].Type)
	assert.Zero(t, msgs[len(msgs)-1].ExitCode)
	assert.ErrorIs(t, sh.client.WriteStdin([]byte("late\n")), ErrStdinClosed)
}

func TestSignalKillsTheWholeProcessGroup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		sig    syscall.Signal
	}{
		{"term", `sleep 60 & echo $!; wait`, syscall.SIGTERM},
		{"kill", `trap '' TERM; sleep 60 & echo $!; while :; do sleep 1; done`, syscall.SIGKILL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := startInProc(t, tc.script, nil)
			s, err := sh.client.Replay(0)
			require.NoError(t, err)
			var child int
			select {
			case m := <-s.C:
				child, err = strconv.Atoi(m.Text)
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("no output")
			}
			require.NoError(t, sh.client.Signal(tc.sig))
			st := waitExited(t, sh.client)
			assert.NotZero(t, st.ExitCode)
			require.Eventually(t, func() bool { return errors.Is(syscall.Kill(child, 0), syscall.ESRCH) },
				10*time.Second, 20*time.Millisecond, "the agent's child must be gone")
		})
	}
	sh := startInProc(t, "sleep 5", nil)
	assert.Error(t, sh.client.Signal(syscall.SIGHUP))
}

func TestStatusReportsExitCodeAndStderr(t *testing.T) {
	sh := startInProc(t, "echo out; echo oops >&2; exit 3", nil)
	st := waitExited(t, sh.client)
	assert.Equal(t, 3, st.ExitCode)
	assert.NotNil(t, st.ExitedAt)
	assert.Equal(t, []string{"oops"}, st.StderrTail)
	assert.Equal(t, sh.spec.SocketPath, st.Socket)
	assert.NotZero(t, st.AgentPID)
	assert.Equal(t, st.AgentPID, st.AgentPGID)
	assert.False(t, st.StartedAt.IsZero())
	assert.False(t, st.LastOutputAt.IsZero())

	onDisk, err := ReadStatus(sh.spec.RunDir)
	require.NoError(t, err)
	assert.True(t, onDisk.Exited)
	assert.Equal(t, 3, onDisk.ExitCode)
	assert.Equal(t, sh.spec.SocketPath, onDisk.Socket)
	assert.EqualValues(t, 4, onDisk.LogSize)
}

func TestStderrTailKeepsTheLastTwentyLines(t *testing.T) {
	sh := startInProc(t, "i=1; while [ $i -le 30 ]; do echo e$i >&2; i=$((i+1)); done", nil)
	st := waitExited(t, sh.client)
	require.Len(t, st.StderrTail, StderrTailLines)
	assert.Equal(t, "e11", st.StderrTail[0])
	assert.Equal(t, "e30", st.StderrTail[StderrTailLines-1])
}

func TestAckOfFinalOffsetStopsTheShim(t *testing.T) {
	sh := startInProc(t, "echo one; echo two", nil)
	st := waitExited(t, sh.client)

	require.NoError(t, sh.client.Ack(st.LogSize-1))
	select {
	case <-sh.done:
		t.Fatal("shim exited before the final offset was acked")
	case <-time.After(150 * time.Millisecond):
	}
	assert.Error(t, sh.client.Ack(st.LogSize+1), "an ack beyond the log is refused")

	require.NoError(t, sh.client.Ack(st.LogSize))
	select {
	case <-sh.done:
		require.NoError(t, sh.err)
	case <-time.After(5 * time.Second):
		t.Fatal("shim did not exit after the final ack")
	}
	_, err := os.Stat(sh.spec.SocketPath)
	assert.True(t, os.IsNotExist(err), "socket is removed")
	_, err = os.Stat(StreamPath(sh.spec.RunDir))
	assert.NoError(t, err, "the log stays")
}

func TestShimExitsAfterGraceWhenNeverAcked(t *testing.T) {
	sh := startInProc(t, "echo done", func(s *Spec) { s.Grace = 150 * time.Millisecond })
	select {
	case <-sh.done:
		require.NoError(t, sh.err)
	case <-time.After(5 * time.Second):
		t.Fatal("shim did not exit after its grace period")
	}
	_, err := os.Stat(sh.spec.SocketPath)
	assert.True(t, os.IsNotExist(err))
}

func TestShimStaysWhileAgentRunsPastGrace(t *testing.T) {
	sh := startInProc(t, "sleep 0.5; echo late", func(s *Spec) { s.Grace = 100 * time.Millisecond })
	select {
	case <-sh.done:
		t.Fatal("the grace period must start at the agent's exit")
	case <-time.After(300 * time.Millisecond):
	}
	st := waitExited(t, sh.client)
	assert.EqualValues(t, 5, st.LogSize)
}

func TestShimDrainsStdoutWithoutAClient(t *testing.T) {
	const lineCount = 50000
	line := strings.Repeat("x", 63)
	sh := startInProc(t, fmt.Sprintf("yes %s | head -n %d", line, lineCount), nil)
	// No client connects until the agent is done; far more than a 64 KiB pipe buffer was written.
	require.Eventually(t, func() bool {
		st, err := ReadStatus(sh.spec.RunDir)
		return err == nil && st.Exited
	}, 20*time.Second, 20*time.Millisecond)
	st, err := sh.client.Status()
	require.NoError(t, err)
	assert.Zero(t, st.ExitCode)
	assert.EqualValues(t, lineCount*(len(line)+1), st.LogSize)
	assert.Greater(t, st.LogSize, int64(1<<20))
}

func TestAgentSurvivesClientDisconnectAndReplaysFromStoredOffset(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "gate")
	sh := startInProc(t, fmt.Sprintf("echo one; while [ ! -f %s ]; do sleep 0.02; done; echo two", gate), nil)

	first, err := sh.client.Replay(0)
	require.NoError(t, err)
	m := <-first.C
	require.Equal(t, "one", m.Text)
	stored := m.Next
	pid := func() int { st, err := sh.client.Status(); require.NoError(t, err); return st.AgentPID }()
	require.NoError(t, sh.client.Close()) // the daemon goes away

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, syscall.Kill(pid, 0), "the agent keeps running")

	c2 := dialRetry(t, sh.spec.SocketPath)
	st, err := c2.Status()
	require.NoError(t, err)
	assert.False(t, st.Exited)
	require.NoError(t, os.WriteFile(gate, nil, 0o600))
	s2, err := c2.Replay(stored)
	require.NoError(t, err)
	msgs := collect(t, s2)
	assert.Equal(t, []string{"two"}, lines(msgs))
	assert.Equal(t, MsgEOF, msgs[len(msgs)-1].Type)
}

func TestStartFailureIsReported(t *testing.T) {
	dataDir := t.TempDir()
	spec := Spec{
		RunID: "nocmd", RunDir: RunDir(dataDir, "nocmd"), SocketPath: filepath.Join(dataDir, "x.sock"),
		Command: []string{filepath.Join(dataDir, "does-not-exist")}, Env: shimEnv(),
	}
	err := Run(context.Background(), spec)
	require.Error(t, err)
	st, rerr := ReadStatus(spec.RunDir)
	require.NoError(t, rerr)
	assert.Contains(t, st.StartError, "does-not-exist")
}

func TestSocketPathStaysShortWithALongDataDir(t *testing.T) {
	short := t.TempDir()
	p, err := SocketPath(short, "run-1")
	require.NoError(t, err)
	if len(filepath.Join(short, "s", "12345678.sock")) <= maxSocketPath {
		assert.Equal(t, filepath.Join(short, "s"), filepath.Dir(p))
	}
	assert.Regexp(t, `/[0-9a-f]{8}\.sock$`, p)

	long := filepath.Join(t.TempDir(), strings.Repeat("deep-directory-name/", 8))
	p, err = SocketPath(long, "run-1")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(p), 100, p)
	fi, err := os.Stat(filepath.Dir(p))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm())

	other, err := SocketPath(long, "run-2")
	require.NoError(t, err)
	assert.NotEqual(t, p, other)
}

func TestStartRunsDetachedShim(t *testing.T) {
	dataDir := t.TempDir()
	runID := "detached-1"
	sock, err := SocketPath(dataDir, runID)
	require.NoError(t, err)
	spec := Spec{
		RunID: runID, RunDir: RunDir(dataDir, runID), SocketPath: sock,
		Command: []string{"sh", "-c", "echo hello; echo SECRET=$ONETT_T"}, Env: append(shimEnv(), "ONETT_T=x"),
	}
	l := Launch{Command: []string{os.Args[0]}, Env: append(shimEnv(), reexecEnv+"=1", "GORACE=atexit_sleep_ms=0")}
	c, err := Start(context.Background(), l, spec)
	require.NoError(t, err)
	defer c.Close()

	st, err := c.Status()
	require.NoError(t, err)
	assert.NotEqual(t, os.Getpid(), st.ShimPID)
	sid, err := syscall.Getsid(st.ShimPID)
	require.NoError(t, err)
	assert.Equal(t, st.ShimPID, sid, "the shim leads its own session, detached from the caller")
	_, err = os.Stat(filepath.Join(spec.RunDir, specName))
	assert.True(t, os.IsNotExist(err), "the spec file holds the environment and is removed")

	s, err := c.Replay(0)
	require.NoError(t, err)
	msgs := collect(t, s)
	assert.Equal(t, []string{"hello", "SECRET=x"}, lines(msgs))
	final := msgs[len(msgs)-1]
	require.Equal(t, MsgEOF, final.Type)
	require.NoError(t, c.Ack(final.Offset))
	require.Eventually(t, func() bool { return errors.Is(syscall.Kill(st.ShimPID, 0), syscall.ESRCH) },
		5*time.Second, 20*time.Millisecond, "the shim exits after the final ack")
}

func TestStartReportsAnAgentThatCannotStart(t *testing.T) {
	dataDir := t.TempDir()
	sock, err := SocketPath(dataDir, "bad-1")
	require.NoError(t, err)
	spec := Spec{
		RunID: "bad-1", RunDir: RunDir(dataDir, "bad-1"), SocketPath: sock,
		Command: []string{"/nonexistent/agent"}, Env: shimEnv(),
	}
	l := Launch{Command: []string{os.Args[0]}, Env: append(shimEnv(), reexecEnv+"=1", "GORACE=atexit_sleep_ms=0")}
	_, err = Start(context.Background(), l, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nonexistent/agent")
}
