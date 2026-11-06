package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A disabled agent is not in the factory the orchestrator is told about, so it cannot be
// handed work by name.
func TestChatInstructionsOmitDisabledAgents(t *testing.T) {
	r := ctNewRig(t)
	improver := r.cfg.Agents["platform-engineer"]
	improver.Disabled = true
	r.cfg.Agents["platform-engineer"] = improver
	r.cfg.Routing["improve"] = "senior-engineer"

	text := factorySnapshot(r.cfg)
	assert.NotContains(t, text, "**platform-engineer**")
	assert.NotContains(t, text, "platform-engineer", "no line mentions the disabled agent")
	assert.Contains(t, text, "- **senior-engineer** (codex) — ")
	assert.Contains(t, text, "handles: bugfix, chore, feature, improve, issue", "the kind it was routed to now shows on the agent that handles it")
}
