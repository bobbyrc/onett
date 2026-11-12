package shimtest_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/runshim/shimtest"
)

const (
	// roleEnv makes the test binary act as a shim, or as a stand-in for a test binary that starts
	// one (the launcher), instead of running the tests.
	roleEnv = "ONETT_SHIMTEST_ROLE"
	// dataEnv is the launcher's data directory.
	dataEnv = "ONETT_SHIMTEST_DATA"
)

func TestMain(m *testing.M) {
	switch os.Getenv(roleEnv) {
	case "shim":
		os.Exit(shimtest.Main())
	case "launcher":
		os.Exit(launcher())
	}
	os.Exit(m.Run())
}

// startShim starts a detached shim, this binary in its shim role, whose agent does not finish by
// itself within the test. The agent is a plain sleep, so it still ends if everything else fails.
func startShim(dataDir, runID string) (*runshim.Client, runshim.Status, error) {
	sock, err := runshim.SocketPath(dataDir, runID)
	if err != nil {
		return nil, runshim.Status{}, err
	}
	path := "PATH=" + os.Getenv("PATH")
	c, err := runshim.Start(context.Background(),
		runshim.Launch{Command: []string{os.Args[0]}, Env: []string{path, roleEnv + "=shim", "GORACE=atexit_sleep_ms=0"}},
		runshim.Spec{
			RunID: runID, RunDir: runshim.RunDir(dataDir, runID), SocketPath: sock,
			Command: []string{"sleep", "300"}, Env: []string{path},
		})
	if err != nil {
		return nil, runshim.Status{}, err
	}
	st, err := c.Status()
	if err != nil {
		_ = c.Close()
		return nil, runshim.Status{}, err
	}
	return c, st, nil
}

// launcher stands for a test binary: it starts a shim, prints "<shim pid> <agent pid>" and then
// hangs until it is killed, as a test that never returns does. It also ends when the real test
// binary that started it is gone, so it cannot itself keep the shim alive after a killed run.
func launcher() int {
	parent := os.Getppid()
	_, st, err := startShim(os.Getenv(dataEnv), "leak-1")
	if err != nil {
		fmt.Fprintln(os.Stderr, "launcher:", err)
		return 1
	}
	fmt.Printf("%d %d\n", st.ShimPID, st.AgentPID)
	for os.Getppid() == parent {
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

func gone(pid int) bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }

// A test binary that is killed (as `go test` does on a timeout) runs no cleanup. The shim it
// started, and that shim's agent, must stop by themselves.
func TestShimAndAgentStopWhenTheTestBinaryDies(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), roleEnv+"=launcher", dataEnv+"="+t.TempDir())
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	var shim, agent int
	_, err = fmt.Fscan(out, &shim, &agent)
	require.NoError(t, err, "the launcher did not report its shim")
	// Only if the check below fails: these two PIDs were reported by our own launcher.
	t.Cleanup(func() {
		if t.Failed() {
			_ = syscall.Kill(agent, syscall.SIGKILL)
			_ = syscall.Kill(shim, syscall.SIGKILL)
		}
	})
	require.NoError(t, syscall.Kill(shim, 0), "the shim is running")
	require.NoError(t, syscall.Kill(agent, 0), "the agent is running")

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	require.Eventually(t, func() bool { return gone(shim) && gone(agent) }, 20*time.Second, 20*time.Millisecond,
		"the shim and its agent must not outlive the test binary that started them")
}

// StopAll is the cleanup for a test that returns normally: it leaves no shim and no agent.
func TestStopAllStopsTheShimAndItsAgent(t *testing.T) {
	dataDir := t.TempDir()
	c, st, err := startShim(dataDir, "stop-1")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.NoError(t, syscall.Kill(st.ShimPID, 0), "the shim is running")
	require.NoError(t, syscall.Kill(st.AgentPID, 0), "the agent is running")

	shimtest.StopAll(t, dataDir)
	require.True(t, gone(st.ShimPID), "the shim is gone when StopAll returns")
	require.Eventually(t, func() bool { return gone(st.AgentPID) }, 5*time.Second, 10*time.Millisecond,
		"the agent went with its shim")
}

// A data directory in which no shim ever ran is fine.
func TestStopAllWithNoRuns(t *testing.T) {
	shimtest.StopAll(t, t.TempDir())
}
