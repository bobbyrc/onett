package harness

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/runshim/shimtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shimReexec makes the test binary act as `onett run-shim`, so these tests need no built onett.
const shimReexec = "ONETT_HARNESS_TEST_RUNSHIM"

func TestMain(m *testing.M) {
	if os.Getenv(shimReexec) == "1" {
		os.Exit(shimtest.Main())
	}
	os.Exit(m.Run())
}

func testShimOptions(t *testing.T) *ShimOptions {
	t.Helper()
	dir := t.TempDir()
	// Stop the test's shims and wait for them before the temp directory is removed: a shim writes
	// its final status after the daemon's last call, and one whose test failed would keep running.
	t.Cleanup(func() { shimtest.StopAll(t, dir) })
	return &ShimOptions{
		DataDir: dir,
		Launch: runshim.Launch{
			Command: []string{os.Args[0]},
			// The race detector otherwise sleeps a second when the shim process exits.
			Env: []string{"PATH=" + os.Getenv("PATH"), shimReexec + "=1", "GORACE=atexit_sleep_ms=0"},
		},
		Grace: time.Minute,
	}
}

func startShimSh(t *testing.T, script string, extra ProcSpec) *Proc {
	t.Helper()
	if extra.RunID == "" {
		extra.RunID = "run-" + strings.ReplaceAll(t.Name(), "/", "-")
	}
	extra.Shim = testShimOptions(t)
	return startSh(t, script, extra)
}

// TestShimMatchesDirectProcess runs the same scripts both ways and expects the same events,
// exit code, result and stderr tail.
func TestShimMatchesDirectProcess(t *testing.T) {
	for name, script := range map[string]string{
		"output":    "echo one; echo; echo two; echo oops >&2",
		"exit code": "echo partial; exit 7",
		"no output": "true",
		"crlf":      `printf 'a\r\nb\n'`,
	} {
		t.Run(name, func(t *testing.T) {
			var codes [2]int
			mk := func(i int) ProcSpec {
				return ProcSpec{OnExit: func(code int, _ error) (Result, error) {
					codes[i] = code
					return Result{FinalText: "done"}, nil
				}}
			}
			direct := startSh(t, script, mk(0))
			dEvents := drain(direct)
			dRes, _ := direct.Wait()

			shimmed := startShimSh(t, script, mk(1))
			sEvents := drain(shimmed)
			sRes, err := shimmed.Wait()
			require.NoError(t, err)

			// Stdout and stderr are separate pipes, so only the order within each is fixed.
			text := func(evs []Event, stderr bool) (out []string) {
				for _, e := range evs {
					if (e.Kind == domain.LogStderr) == stderr {
						out = append(out, string(e.Kind)+"|"+e.Text)
					}
				}
				return out
			}
			assert.Equal(t, text(dEvents, false), text(sEvents, false))
			assert.Equal(t, text(dEvents, true), text(sEvents, true))
			assert.Equal(t, dRes, sRes)
			assert.Equal(t, codes[0], codes[1])
			assert.Equal(t, direct.StderrTail(), shimmed.StderrTail())
			for _, ev := range sEvents {
				assert.False(t, ev.Time.IsZero())
			}
		})
	}
}

func TestShimFailsToStartMissingCommand(t *testing.T) {
	_, err := StartProcess(context.Background(), ProcSpec{
		Command: []string{"/nonexistent/agent"}, Shim: testShimOptions(t), RunID: "missing",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nonexistent/agent")
}

func TestShimCancelStopsTheProcessGroup(t *testing.T) {
	p := startShimSh(t, "sleep 60 & echo $!; wait", ProcSpec{})
	var childPID string
	select {
	case ev := <-p.Events():
		childPID = ev.Text
	case <-time.After(5 * time.Second):
		t.Fatal("no output")
	}
	p.Cancel()
	p.Cancel() // idempotent
	done := make(chan struct{})
	go func() { drain(p); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled process did not stop")
	}
	res, err := p.Wait()
	require.NoError(t, err)
	assert.NotZero(t, res.ExitCode)
	assert.True(t, res.IsError)

	pid := 0
	for _, c := range childPID {
		pid = pid*10 + int(c-'0')
	}
	require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 5*time.Second, 20*time.Millisecond)
}

func TestShimContextCancelCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, err := StartProcess(ctx, ProcSpec{
		Command: []string{"sleep", "60"}, ParseLine: textParser, Shim: testShimOptions(t), RunID: "ctx-cancel",
	})
	require.NoError(t, err)
	cancel()
	done := make(chan struct{})
	go func() { drain(p); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("context cancel did not stop the process")
	}
}

func TestShimStdinWriteAndClose(t *testing.T) {
	p := startShimSh(t, `while read l; do echo "got $l"; done`, ProcSpec{KeepStdinOpen: true, Stdin: "first\n"})
	require.NoError(t, p.WriteStdin([]byte("second\n")))
	p.CloseStdin()
	assert.ErrorIs(t, p.WriteStdin([]byte("late\n")), ErrInputClosed)

	var got []string
	for _, ev := range drain(p) {
		got = append(got, ev.Text)
	}
	assert.Equal(t, []string{"got first", "got second"}, got)
	res, err := p.Wait()
	require.NoError(t, err)
	assert.Zero(t, res.ExitCode)
}

func TestShimWithoutKeepStdinOpenRejectsWrites(t *testing.T) {
	p := startShimSh(t, "cat", ProcSpec{Stdin: "prompt\n"})
	assert.ErrorIs(t, p.WriteStdin([]byte("more")), ErrInputClosed)
	var got []string
	for _, ev := range drain(p) {
		got = append(got, ev.Text)
	}
	assert.Equal(t, []string{"prompt"}, got)
}

func TestShimEnvIsTheAgentAllowlist(t *testing.T) {
	t.Setenv("ONETT_TEST_SECRET", "hunter2")
	p := startShimSh(t, `echo "secret=[$ONETT_TEST_SECRET] extra=[$EXTRA]"`, ProcSpec{Env: map[string]string{"EXTRA": "yes"}})
	evs := drain(p)
	require.Len(t, evs, 1)
	assert.Equal(t, "secret=[] extra=[yes]", evs[0].Text)
	assert.Equal(t, domain.LogText, evs[0].Kind)
}
