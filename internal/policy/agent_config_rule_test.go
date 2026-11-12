package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

func TestDefaultRulesRateAgentCLIConfigHigh(t *testing.T) {
	p := config.Policy{Rules: config.DefaultRules(), Gates: testGates()}
	paths := []string{
		".claude/settings.json",
		".claude/settings.local.json",
		".claude/skills/x/SKILL.md",
		".mcp.json",
		".codex/config.toml",
		".codex/hooks.json",
		"web/.claude/settings.json",
		"web/.mcp.json",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			d := Evaluate(p, Input{
				Kind:      domain.KindFeature,
				AgentRisk: domain.RiskLow,
				Files:     []FileChange{{Path: path, Added: 1}},
			})
			assert.Equal(t, domain.RiskHigh, d.Risk)
			assert.Contains(t, d.Reasons, "agent CLI settings, hooks or MCP servers ("+path+")")
			assert.False(t, d.AutoMerge)
		})
	}
}

func TestDefaultRulesLeaveOrdinaryDocsLow(t *testing.T) {
	p := config.Policy{Rules: config.DefaultRules(), Gates: testGates()}
	d := Evaluate(p, Input{
		Kind:      domain.KindFeature,
		AgentRisk: domain.RiskLow,
		Files:     []FileChange{{Path: "docs/notes.md", Added: 1}},
	})
	require.Equal(t, domain.RiskLow, d.Risk)
}
