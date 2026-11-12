package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The README calls the agent the owner talks to the chief of staff; the help text must too.
func TestHelpNamesTheChiefOfStaff(t *testing.T) {
	code, out, errOut := runCLI("help")
	require.Equal(t, 0, code, errOut)
	seen := 0
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || (f[0] != "goal" && f[0] != "chat") {
			continue
		}
		seen++
		assert.Contains(t, line, "chief of staff")
		assert.NotContains(t, line, "orchestrator")
	}
	assert.Equal(t, 2, seen, "the goal and chat lines")
}
