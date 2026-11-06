package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Screenshots are part of done for any web/ change; the roles that write or review it must say so.
func TestBuiltinPromptsRequireUIScreenshots(t *testing.T) {
	e := &Engine{}
	for name, want := range map[string][]string{
		"senior-engineer": {"shots:chat", "shoot-live", "390x844"},
		"visual-designer": {"shots:chat", "shoot-live", "390x844"},
		"code-reviewer":   {"shots:chat", "screenshots", "major"},
	} {
		got, err := e.BuiltinPrompt(name)
		require.NoError(t, err, name)
		for _, s := range want {
			assert.Contains(t, got, s, name)
		}
	}
}
