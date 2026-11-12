package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

// lastFlagValue returns the value after the last occurrence of flag in args.
func lastFlagValue(args []string, flag string) (string, bool) {
	for i := len(args) - 2; i >= 0; i-- {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

func TestBuildArgsNoProjectConfigDropsTreeSettings(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{NoProjectConfig: true})
	value, ok := lastFlagValue(args, "--setting-sources")
	require.True(t, ok)
	assert.Equal(t, "user", value)
	assert.Contains(t, args, "--strict-mcp-config")

	args = buildArgs(harness.Config{Isolate: true}, harness.Spec{NoProjectConfig: true})
	value, ok = lastFlagValue(args, "--setting-sources")
	require.True(t, ok)
	assert.Equal(t, "", value)
	assert.Contains(t, args, "--strict-mcp-config")
}

func TestBuildArgsNoProjectConfigWinsOverExtras(t *testing.T) {
	args := buildArgs(
		harness.Config{Isolate: true, Args: []string{"--setting-sources", "user,project,local"}},
		harness.Spec{NoProjectConfig: true, Args: []string{"--setting-sources", "project", "--mcp-config", "x.json"}},
	)
	value, ok := lastFlagValue(args, "--setting-sources")
	require.True(t, ok)
	assert.Equal(t, "", value)

	mcpIndex := -1
	for i, a := range args {
		if a == "--strict-mcp-config" {
			mcpIndex = i
		}
	}
	xIndex := indexOf(args, "x.json")
	require.NotEqual(t, -1, mcpIndex)
	require.NotEqual(t, -1, xIndex)
	assert.Greater(t, mcpIndex, xIndex, "onett's MCP flag must come after every extra")
}

func TestBuildArgsWithoutNoProjectConfigIsUnchanged(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{})
	assert.NotContains(t, args, "--setting-sources")
	assert.NotContains(t, args, "--strict-mcp-config")

	args = buildArgs(harness.Config{Isolate: true}, harness.Spec{})
	value, ok := lastFlagValue(args, "--setting-sources")
	require.True(t, ok)
	assert.Equal(t, "project,local", value)
}

func TestBuildArgsNoProjectConfigKeepsPermissionMode(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{NoProjectConfig: true})
	assert.NotEqual(t, -1, indexOf(args, "--permission-mode"))
	i := indexOf(args, "--permission-mode")
	require.NotEqual(t, -1, i)
	require.Less(t, i+1, len(args))
	assert.Equal(t, "auto", args[i+1])
}
