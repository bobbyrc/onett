package runshim

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// startTimeout is how long Start waits for a new shim to answer on its socket.
const startTimeout = 15 * time.Second

// Launch says how to start a shim process.
type Launch struct {
	// Command is the shim's command line without the spec path, normally {onett, "run-shim"}.
	Command []string
	// Env is the shim's own environment. The agent's environment is Spec.Env and is unrelated.
	Env []string
}

// Start launches a shim for spec as a detached process (its own session, stdio not tied to the
// caller) and returns a client for it once its socket answers. The shim outlives the caller.
// A shim that cannot start the agent exits at once; Start then returns that error.
func Start(ctx context.Context, l Launch, spec Spec) (*Client, error) {
	if len(l.Command) == 0 {
		return nil, errors.New("start shim: no shim command")
	}
	if err := os.MkdirAll(spec.RunDir, 0o700); err != nil {
		return nil, fmt.Errorf("start shim: %w", err)
	}
	b, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	specPath := spec.RunDir + "/" + specName
	if err := os.WriteFile(specPath, b, 0o600); err != nil { // holds the agent's environment
		return nil, fmt.Errorf("start shim: %w", err)
	}
	logf, err := os.OpenFile(spec.RunDir+"/"+shimLogName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("start shim: %w", err)
	}
	defer logf.Close()

	cmd := exec.Command(l.Command[0], append(append([]string(nil), l.Command[1:]...), "--spec", specPath)...)
	cmd.Env = l.Env
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start shim: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	deadline := time.NewTimer(startTimeout)
	defer deadline.Stop()
	for {
		if c, err := Dial(spec.SocketPath); err == nil {
			return c, nil
		}
		select {
		case <-exited:
			return nil, fmt.Errorf("start shim: %s", startFailure(spec.RunDir))
		case <-deadline.C:
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("start shim: no answer on %s after %s", spec.SocketPath, startTimeout)
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// startFailure explains why a shim exited before serving, from its status file or its log.
func startFailure(runDir string) string {
	if st, err := ReadStatus(runDir); err == nil && st.StartError != "" {
		return st.StartError
	}
	if b, err := os.ReadFile(runDir + "/" + shimLogName); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return "shim exited: " + strings.TrimSpace(string(b))
	}
	return "shim exited before it was ready"
}

// Main is the `onett run-shim` entry point: args are the arguments after the subcommand, and the
// result is the process exit code. The spec file is removed once read, since it holds the agent's
// environment.
func Main(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("run-shim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specPath := fs.String("spec", "", "path of the JSON spec file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *specPath == "" {
		fmt.Fprintln(stderr, "run-shim: --spec is required")
		return 2
	}
	b, err := os.ReadFile(*specPath)
	if err != nil {
		fmt.Fprintf(stderr, "run-shim: %v\n", err)
		return 1
	}
	_ = os.Remove(*specPath)
	var spec Spec
	if err := json.Unmarshal(b, &spec); err != nil {
		fmt.Fprintf(stderr, "run-shim: bad spec: %v\n", err)
		return 1
	}
	// SIGTERM or SIGINT to the shim stops the agent too, rather than orphaning it.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := Run(ctx, spec); err != nil {
		fmt.Fprintf(stderr, "run-shim: %v\n", err)
		return 1
	}
	return 0
}
