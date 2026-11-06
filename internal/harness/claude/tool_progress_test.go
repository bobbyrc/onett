package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// A tool_progress heartbeat is still emitted as a log line: it is the only output while a long
// command runs, so it keeps the stall clock fresh. The UI hides it from the conversation.
func TestToolProgressHeartbeatStaysAsSystemLine(t *testing.T) {
	line := `{"type":"tool_progress","tool_use_id":"toolu_1-heartbeat-0","tool_name":"Bash","elapsed_time_seconds":30,"heartbeat":true,"session_id":"s1"}`
	evs := newParser("").parseLine(line)
	require.Len(t, evs, 1)
	assert.Equal(t, domain.LogSystem, evs[0].Kind)
	assert.Contains(t, evs[0].Text, `"type":"tool_progress"`)
}
