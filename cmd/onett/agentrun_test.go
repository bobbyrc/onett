package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setAgentRun makes the process look like an agent session: the variables onett sets for every
// agent it starts. Tests call it with t.Setenv, so they are restored afterwards.
func setAgentRun(t *testing.T, runID, role string) {
	t.Helper()
	t.Setenv("ONETT_RUN_ID", runID)
	t.Setenv("ONETT_ROLE", role)
}

func TestCLIInsideAgentRunNeverReadsTheTokenFile(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "token"), []byte("owner-token\n"), 0o600))
	var cfg config.Config
	cfg.Server.DataDir = dataDir

	t.Run("outside an agent run the token file is read", func(t *testing.T) {
		t.Setenv("ONETT_TOKEN", "")
		setAgentRun(t, "", "")
		token, err := apiToken(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "owner-token", token)
	})

	t.Run("a worker run without a token is refused", func(t *testing.T) {
		t.Setenv("ONETT_TOKEN", "")
		setAgentRun(t, "run-1", "")
		_, err := apiToken(&cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agent run")
	})

	t.Run("a role without a run id is refused too", func(t *testing.T) {
		t.Setenv("ONETT_TOKEN", "")
		setAgentRun(t, "", "engineer")
		_, err := apiToken(&cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agent run")
	})

	t.Run("the orchestrator uses the token it is given", func(t *testing.T) {
		t.Setenv("ONETT_TOKEN", "agent-token")
		setAgentRun(t, "run-1", "")
		token, err := apiToken(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "agent-token", token)
	})
}

func TestOwnerCommandsRefuseInsideAgentRun(t *testing.T) {
	for _, args := range [][]string{
		{"token"},
		{"user", "reset-password", "someone"},
		{"project", "add", "demo"},
		{"service", "status"},
		{"init"},
		{"open"},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			setAgentRun(t, "run-1", "orchestrator")
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)
			assert.Equal(t, 1, code)
			assert.Contains(t, stderr.String(), "does not run inside an agent run")
			assert.Empty(t, stdout.String())
		})
	}
}

func TestAPICommandsStayAllowedInsideAgentRun(t *testing.T) {
	setAgentRun(t, "", "orchestrator")
	for _, args := range [][]string{
		{"status"},
		{"task", "retry", "x"},
		{"project", "pause", "x"},
		{"project", "resume", "x"},
		{"approve", "x"},
		{"resume"},
		{"loop", "run", "x"},
		{"version"},
	} {
		assert.NoError(t, refuseInAgentRun(args), "args %v", args)
	}
}

func TestServeInsideAgentRunIsForWorkersOnly(t *testing.T) {
	setAgentRun(t, "run-1", "orchestrator")
	assert.Error(t, refuseInAgentRun([]string{"serve"}), "the orchestrator must not start a second daemon")

	setAgentRun(t, "run-1", "engineer")
	assert.NoError(t, refuseInAgentRun([]string{"serve"}), "a worker may start a throwaway daemon")

	setAgentRun(t, "run-1", "")
	assert.NoError(t, refuseInAgentRun([]string{"serve"}))
}

func TestRunShimRefusedInsideAgentRun(t *testing.T) {
	setAgentRun(t, "", "")
	assert.NoError(t, refuseInAgentRun([]string{"run-shim"}))

	setAgentRun(t, "run-1", "orchestrator")
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 1, run([]string{"run-shim"}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "does not run inside an agent run")
}

func TestHelpStillWorksInsideAgentRun(t *testing.T) {
	setAgentRun(t, "run-1", "orchestrator")
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 0, run([]string{"help"}, &stdout, &stderr))
	assert.Contains(t, stdout.String(), "Usage:")
	for _, args := range [][]string{{"-h"}, {"-help"}, {"--help"}} {
		assert.NoError(t, refuseInAgentRun(args), "args %v", args)
	}
}

func TestNothingIsRefusedOutsideAnAgentRun(t *testing.T) {
	setAgentRun(t, "", "")
	for _, args := range [][]string{
		{"token"},
		{"user", "reset-password", "x"},
		{"project", "add", "x"},
	} {
		assert.NoError(t, refuseInAgentRun(args), "args %v", args)
	}
}

func TestUnlistedCommandIsRefusedInsideAgentRun(t *testing.T) {
	setAgentRun(t, "run-1", "")
	assert.Error(t, refuseInAgentRun([]string{"some-future-command"}))
}

func TestWorkersGetNoCLIAndNoToken(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	agent, orchestrator := serveEnvs("/opt/onett/bin/onett", "127.0.0.1:7777", "secret-token")

	assert.Len(t, agent, 0)
	assert.Equal(t, auth.AgentToken("secret-token"), orchestrator["ONETT_TOKEN"])
	assert.NotEqual(t, "secret-token", orchestrator["ONETT_TOKEN"])
	assert.True(t, strings.HasPrefix(orchestrator["PATH"], "/opt/onett/bin"+string(os.PathListSeparator)))
	assert.NotEmpty(t, orchestrator["ONETT_API"])
}

func TestOpenAddsTheSetupCodeOnlyWhileNoAccountExists(t *testing.T) {
	t.Setenv("ONETT_TOKEN", "owner-token")

	stateServer := func(setupRequired bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/auth/state", r.URL.Path)
			fmt.Fprintf(w, `{"setup_required":%t,"user":null}`, setupRequired)
		}))
	}

	srv := stateServer(true)
	defer srv.Close()
	t.Setenv("ONETT_API", srv.URL)
	assert.Equal(t, auth.SetupCode("owner-token"), setupCode(nil))

	srv2 := stateServer(false)
	defer srv2.Close()
	t.Setenv("ONETT_API", srv2.URL)
	assert.Equal(t, "", setupCode(nil))

	closed := stateServer(true)
	closedURL := closed.URL
	closed.Close()
	t.Setenv("ONETT_API", closedURL)
	assert.Equal(t, "", setupCode(nil))
}

func TestReadOnlyCommandsAllowedInsideAgentRun(t *testing.T) {
	setAgentRun(t, "run-1", "orchestrator")
	for _, args := range [][]string{
		{"trigger", "list"},
		{"trigger", "history", "x"},
		{"signals"},
		{"signals", "show", "x"},
		{"friction"},
		{"friction", "show", "x"},
		{"conversations"},
		{"conversations", "show", "x"},
		{"deploys"},
		{"deploys", "show", "x"},
		{"run", "list"},
		{"run", "show", "x"},
		{"run", "logs", "x"},
	} {
		assert.NoError(t, refuseInAgentRun(args), "args %v", args)
	}
}

func TestBackupRefusedInsideAgentRun(t *testing.T) {
	setAgentRun(t, "run-1", "orchestrator")
	target := filepath.Join(t.TempDir(), "copy.db")
	var stdout, stderr bytes.Buffer
	code := run([]string{"backup", target}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "does not run inside an agent run")
	_, err := os.Stat(target)
	assert.True(t, os.IsNotExist(err), "backup must not write the file, got err %v", err)
}

func TestEveryCommandIsDecidedForAgentRuns(t *testing.T) {
	refused := map[string]bool{
		"serve": true, "init": true, "token": true, "open": true, "user": true,
		"backup": true, "service": true,
	}
	for _, c := range commandList() {
		allowed := agentRunCommands[c.name]
		assert.False(t, allowed && refused[c.name], "command %q is in both sets", c.name)
		assert.True(t, allowed || refused[c.name],
			"command %q is neither allowed nor listed as refused inside an agent run: decide which", c.name)
	}
}
