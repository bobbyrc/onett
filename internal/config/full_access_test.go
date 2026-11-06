package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHarnessFullAccessDefaultsOffAndParses(t *testing.T) {
	cfg, err := Parse([]byte("harnesses:\n  claude:\n    type: claude-code\n  codex:\n    type: codex\n"), "")
	require.NoError(t, err)
	assert.False(t, cfg.Harness["claude"].ClaudeFullAccess())
	assert.False(t, cfg.Harness["codex"].CodexFullAccess())

	cfg, err = Parse([]byte("harnesses:\n  claude:\n    type: claude-code\n    claude:\n      full_access: true\n  codex:\n    type: codex\n    codex:\n      full_access: true\n"), "")
	require.NoError(t, err)
	assert.True(t, cfg.Harness["claude"].ClaudeFullAccess())
	assert.True(t, cfg.Harness["codex"].CodexFullAccess())
}

func TestHarnessProviderSectionMustMatchType(t *testing.T) {
	_, err := Parse([]byte("harnesses:\n  x:\n    type: claude-code\n    codex:\n      full_access: true\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harnesses.x.codex: only valid when type is codex")

	_, err = Parse([]byte("harnesses:\n  x:\n    type: exec\n    command: [sh]\n    claude:\n      full_access: true\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harnesses.x.claude: only valid when type is claude-code")
}

func TestHarnessProviderSectionRejectsUnknownKey(t *testing.T) {
	_, err := Parse([]byte("harnesses:\n  x:\n    type: codex\n    codex:\n      full_acess: true\n"), "")
	require.Error(t, err)
}

func TestSchemaShowsProviderSections(t *testing.T) {
	var harnesses SchemaNode
	for _, f := range Schema().Fields {
		if f.Key == "harnesses" {
			harnesses = f
		}
	}
	require.NotNil(t, harnesses.Item)
	for _, key := range []string{"claude", "codex"} {
		var sec *SchemaNode
		for i, f := range harnesses.Item.Fields {
			if f.Key == key {
				sec = &harnesses.Item.Fields[i]
			}
		}
		require.NotNil(t, sec, key)
		require.Equal(t, "section", sec.Type)
		require.Len(t, sec.Fields, 1)
		fa := sec.Fields[0]
		assert.Equal(t, "full_access", fa.Key)
		assert.Equal(t, "bool", fa.Type)
		assert.Equal(t, false, fa.Default)
		assert.Contains(t, fa.Help, "Off:")
	}
}
