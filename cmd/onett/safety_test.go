package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain makes the whole package safe to test on a real Mac. Before any test runs:
//   - launchAgentsDir points at a fresh temp directory, so no plist can land in ~/Library/LaunchAgents;
//   - HOME points at a fresh temp directory, so nothing reads or writes the developer's home;
//   - the agent-session variables (ONETT_GOAL, ONETT_CONVERSATION_ID, ONETT_ROLE, ONETT_RUN_ID) are
//     unset, so tests give the same result when run from inside an onett agent session;
//   - commandRunner refuses every command, so no test can run launchctl, git, gh or any other
//     program unless it installs its own fake with fakeRunner.
//
// The temp directories are removed after the tests. os.Exit skips defers, so cleanup is explicit.
func TestMain(m *testing.M) {
	agents, err := os.MkdirTemp("", "onett-test-agents-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create test LaunchAgents dir: %v\n", err)
		os.Exit(1)
	}
	home, err := os.MkdirTemp("", "onett-test-home-*")
	if err != nil {
		_ = os.RemoveAll(agents)
		fmt.Fprintf(os.Stderr, "create test HOME: %v\n", err)
		os.Exit(1)
	}

	launchAgentsDir = agents
	if err := os.Setenv("HOME", home); err != nil {
		_ = os.RemoveAll(agents)
		_ = os.RemoveAll(home)
		fmt.Fprintf(os.Stderr, "set test HOME: %v\n", err)
		os.Exit(1)
	}
	for _, k := range []string{"ONETT_GOAL", "ONETT_CONVERSATION_ID", "ONETT_ROLE", "ONETT_RUN_ID"} {
		_ = os.Unsetenv(k)
	}
	commandRunner = func(_ context.Context, name string, args ...string) (string, error) {
		return "", fmt.Errorf("unstubbed command in test: %s", strings.TrimSpace(name+" "+strings.Join(args, " ")))
	}

	code := m.Run()

	_ = os.RemoveAll(agents)
	_ = os.RemoveAll(home)
	os.Exit(code)
}
