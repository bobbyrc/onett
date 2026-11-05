package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

// The result's usage splits input into fresh, cache-written and cache-read tokens; the run
// records the two cached parts so the cache hit rate can be shown.
func TestResultReportsCacheTokens(t *testing.T) {
	_, res := replay(t, "testdata/simple.jsonl")
	assert.Equal(t, int64(11382), res.CachedInputTokens)
	assert.Equal(t, int64(47175), res.CacheWriteTokens)
	assert.Equal(t, int64(2+47175+11382), res.InputTokens)

	_, res = replay(t, "testdata/two_results.jsonl")
	assert.Equal(t, int64(2), res.CachedInputTokens, "summed over both results")
	assert.Equal(t, int64(1), res.CacheWriteTokens)
}

func TestBuildArgsIsolateKeepsOwnerSetupOut(t *testing.T) {
	args := buildArgs(harness.Config{Isolate: true, Args: []string{"--setting-sources", "user"}}, harness.Spec{})
	i := indexOf(args, "--strict-mcp-config")
	require.NotEqual(t, -1, i)
	assert.Equal(t, []string{"--strict-mcp-config", "--setting-sources", "project,local", "--setting-sources", "user"}, args[i:i+5],
		"isolation comes before the owner's extra args, which can override it")

	args = buildArgs(harness.Config{}, harness.Spec{})
	assert.Equal(t, -1, indexOf(args, "--strict-mcp-config"))
	assert.Equal(t, -1, indexOf(args, "--setting-sources"))
}

// Two turns of one conversation differ only in the prompt and the session to resume; the prompt
// goes to stdin, so the command line, and with it the cached prefix, is the same.
func TestBuildArgsAreIdenticalAcrossTurns(t *testing.T) {
	cfg := harness.Config{Isolate: true}
	turn := func(prompt string) []string {
		return buildArgs(cfg, harness.Spec{
			Prompt: prompt, Instructions: "same system prompt", Model: "opus", ResumeSessionID: "sess-1",
		})
	}
	assert.Equal(t, turn("first message"), turn("a quite different second message"))
}
