// Package shimtest is for tests that start real run shims by re-executing the test binary. It
// keeps those shims, and the agents behind them, from outliving the test or the test binary.
package shimtest

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/runshim"
)

// Main is `onett run-shim` for a test binary: TestMain calls it when the binary was started as a
// shim, and exits with its result. It is runshim.Main plus one rule a real shim must not have:
// when the process that started the shim is gone, the shim stops and takes its agent with it. A
// real shim outlives the daemon on purpose. A test's shim that outlives a test binary that
// panicked, timed out or was killed is a leak, and no t.Cleanup runs in those cases.
func Main() int {
	// shortcut: a test binary that dies before this line is not noticed, because the parent is
	// already someone else when it is first read. Pass the parent's PID in if that ever happens.
	parent := os.Getppid()
	go func() {
		for os.Getppid() == parent {
			time.Sleep(200 * time.Millisecond)
		}
		// SIGTERM is the shim's own stop path: it kills the agent's process group, then exits.
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	return runshim.Main(os.Args[1:], os.Stderr)
}

// StopAll stops every shim that has a status.json under dataDir, and with it the shim's agent,
// then waits until the shim processes are gone. Nothing is then left running, or still writing
// into dataDir, when the test's temp directory is removed. Register it with t.Cleanup right after
// creating the directory, so it runs after the test's other cleanups.
//
// A PID read from a file may belong to another process by now, so a shim is signalled only while
// the process answering on its socket is that PID (runshim.DialStatus).
func StopAll(t testing.TB, dataDir string) {
	t.Helper()
	dirs, _ := filepath.Glob(filepath.Join(dataDir, "runs", "*"))
	for _, d := range dirs {
		st, err := runshim.ReadStatus(d)
		if err != nil || st.ShimPID <= 1 {
			continue
		}
		if c, err := runshim.DialStatus(st); err == nil {
			_ = c.Close()
			_ = syscall.Kill(st.ShimPID, syscall.SIGTERM)
		}
		// Also for a shim that no longer answers: it may still be writing its last status.
		deadline := time.Now().Add(15 * time.Second)
		for syscall.Kill(st.ShimPID, 0) != syscall.ESRCH {
			if time.Now().After(deadline) {
				t.Errorf("shim %d of run %s is still running", st.ShimPID, filepath.Base(d))
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
