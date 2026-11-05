package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestartAfterShutdownUnderLaunchdJustExits(t *testing.T) {
	execSelf = func(string, []string, []string) error { t.Fatal("must not exec under launchd"); return nil }
	t.Cleanup(func() { execSelf = nil })

	env := func(k string) string {
		if k == "XPC_SERVICE_NAME" {
			return serviceLabel
		}
		return ""
	}
	require.NoError(t, restartAfterShutdown("/bin/onett", []string{"onett", "serve"}, nil, env))
}

func TestRestartAfterShutdownReExecsWhenUnsupervised(t *testing.T) {
	var gotExe string
	var gotArgs, gotEnv []string
	execSelf = func(exe string, args, env []string) error { gotExe, gotArgs, gotEnv = exe, args, env; return nil }
	t.Cleanup(func() { execSelf = nil })

	args, env := []string{"onett", "serve", "--config", "x.yaml"}, []string{"A=1"}
	require.NoError(t, restartAfterShutdown("/bin/onett", args, env, func(string) string { return "" }))
	assert.Equal(t, "/bin/onett", gotExe)
	assert.Equal(t, args, gotArgs)
	assert.Equal(t, env, gotEnv)
}

func TestRestartAfterShutdownReportsExecFailure(t *testing.T) {
	execSelf = func(string, []string, []string) error { return errors.New("boom") }
	t.Cleanup(func() { execSelf = nil })

	err := restartAfterShutdown("/bin/onett", nil, nil, func(string) string { return "" })
	assert.ErrorContains(t, err, "boom")
}
