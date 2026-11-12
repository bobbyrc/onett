package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinEntryWithoutPromptSaysWhatToWrite(t *testing.T) {
	_, err := Parse([]byte("agents:\n  senior-engineer:\n    harness: codex\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `agents.senior-engineer.prompt: must not be empty; an entry here replaces the built-in role whole, so add "prompt: builtin:senior-engineer"`)
}

func TestBuiltinEntryWithoutHarnessSaysWhatToWrite(t *testing.T) {
	_, err := Parse([]byte("agents:\n  code-reviewer:\n    model: opus\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `agents.code-reviewer.harness: must not be empty; an entry here replaces the built-in role whole, so add "harness: claude"`)
	assert.Contains(t, err.Error(), `agents.code-reviewer.prompt: must not be empty; an entry here replaces the built-in role whole, so add "prompt: builtin:code-reviewer"`)
	assert.NotContains(t, err.Error(), `unknown harness ""`)
}

func TestBuiltinEntryWithHarnessAndPromptLoads(t *testing.T) {
	cfg, err := Parse([]byte("agents:\n  senior-engineer:\n    harness: claude\n    prompt: builtin:senior-engineer\n"), "")
	require.NoError(t, err)
	assert.Equal(t, "claude", cfg.Agents["senior-engineer"].Harness)
	assert.Equal(t, "", cfg.Agents["senior-engineer"].Model, "the entry replaced the built-in whole")
}

func TestBuiltinEntryUnknownHarnessMessageUnchanged(t *testing.T) {
	_, err := Parse([]byte("agents:\n  senior-engineer:\n    harness: nowhere\n    prompt: builtin:senior-engineer\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `agents.senior-engineer.harness: unknown harness "nowhere"`)
}

func TestCustomEntryWithoutPromptMessageUnchanged(t *testing.T) {
	_, err := Parse([]byte("agents:\n  docs:\n    harness: claude\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents.docs.prompt: must not be empty")
	assert.NotContains(t, err.Error(), "built-in role")
}

func TestBuiltinEntryUnderOldNameSaysWhatToWrite(t *testing.T) {
	_, err := Parse([]byte("agents:\n  implementer:\n    harness: codex\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `so add "prompt: builtin:senior-engineer"`)
}
