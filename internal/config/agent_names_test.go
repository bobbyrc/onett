package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestCanonicalAgentName(t *testing.T) {
	for old, want := range map[string]string{
		"orchestrator": "chief-of-staff", "implementer": "senior-engineer", "reviewer": "code-reviewer",
		"triager": "tech-lead", "improver": "platform-engineer", "sre": "sre", "custom": "custom",
	} {
		assert.Equal(t, want, CanonicalAgentName(old), old)
	}
	assert.Equal(t, []string{"implementer"}, LegacyAgentNames("senior-engineer"))
	assert.Empty(t, LegacyAgentNames("sre"))
}

func TestBuiltinAgentsAreRolesWithTitles(t *testing.T) {
	b := BuiltinAgents()
	require.Len(t, b, 8)
	for name, a := range b {
		assert.NotEmpty(t, a.Title, name)
		assert.Equal(t, "builtin:"+name, a.Prompt, name)
		assert.False(t, strings.Contains(strings.ToLower(a.Description), "model"), name+" description should not name the model")
	}
	assert.Equal(t, "Senior Engineer", b["senior-engineer"].Title)
	assert.Equal(t, []string{"PRODUCT.md", "DESIGN.md", "docs/"}, b["product-designer"].Context)
	assert.NotEmpty(t, b["visual-designer"].Tools)
}

func TestOldAgentKeyEditsTheRenamedBuiltin(t *testing.T) {
	cfg, err := Parse([]byte("agents:\n  implementer: {harness: claude, prompt: builtin:implementer, model: x}\n"), "")
	require.NoError(t, err)
	a := cfg.Agents["senior-engineer"]
	assert.Equal(t, "x", a.Model)
	assert.Equal(t, OriginEdited, a.Origin)
	assert.NotContains(t, cfg.Agents, "implementer")
}

func TestOldRoutingValueRoutesToRenamedAgent(t *testing.T) {
	cfg, err := Parse([]byte("routing:\n  feature: implementer\n  question: triager\nprojects:\n  app:\n    repo: bob/app\n    routing:\n      bugfix: reviewer\n"), "")
	require.NoError(t, err)
	name, _, err := cfg.AgentFor(domain.KindFeature, "")
	require.NoError(t, err)
	assert.Equal(t, "senior-engineer", name)
	assert.Equal(t, "tech-lead", cfg.Routing["question"])
	name, _, err = cfg.AgentForProject("app", domain.KindBugfix, "")
	require.NoError(t, err)
	assert.Equal(t, "code-reviewer", name)
}

func TestExplicitOldRoleResolves(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)
	name, _, err := cfg.AgentFor(domain.KindFeature, "implementer")
	require.NoError(t, err)
	assert.Equal(t, "senior-engineer", name)
}

func TestOldAndNewKeyTogetherIsError(t *testing.T) {
	_, err := Parse([]byte("agents:\n  implementer: {model: a}\n  senior-engineer: {model: b}\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents.implementer and agents.senior-engineer")

	_, err = Parse([]byte("projects:\n  app:\n    repo: bob/app\n    agents:\n      reviewer: {model: a}\n      code-reviewer: {model: b}\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "projects.app.agents.reviewer and projects.app.agents.code-reviewer")
}

func TestProjectOverrideUnderOldKeyMerges(t *testing.T) {
	cfg, err := Parse([]byte("projects:\n  app:\n    repo: bob/app\n    agents:\n      implementer: {harness: claude, prompt: builtin:implementer, model: x}\n"), "")
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.AgentsFor("app")["senior-engineer"].Model)
}

func TestTitleContextToolsMakeAnOverrideAnEdit(t *testing.T) {
	for _, field := range []string{"title: Boss", "context: [A.md]", "tools: [\"Bash(go:*)\"]"} {
		cfg, err := Parse([]byte("agents:\n  sre: {harness: claude, prompt: builtin:sre, "+field+"}\n"), "")
		require.NoError(t, err, field)
		assert.Equal(t, OriginEdited, cfg.Agents["sre"].Origin, field)
	}
}

func TestProjectOverrideMergesTitleContextTools(t *testing.T) {
	cfg, err := Parse([]byte("projects:\n  app:\n    repo: bob/app\n    agents:\n      sre: {title: T, context: [X.md], tools: [\"Bash(x:*)\"]}\n"), "")
	require.NoError(t, err)
	a := cfg.AgentsFor("app")["sre"]
	assert.Equal(t, "T", a.Title)
	assert.Equal(t, []string{"X.md"}, a.Context)
	assert.Equal(t, []string{"Bash(x:*)"}, a.Tools)
	assert.Equal(t, "builtin:sre", a.Prompt, "unset fields keep the built-in")
}

func TestToolsOnCodexHarnessWarnsButLoads(t *testing.T) {
	cfg, err := Parse([]byte("agents:\n  senior-engineer: {harness: codex, prompt: builtin:senior-engineer, tools: [\"Bash(x:*)\"]}\n  sre: {harness: claude, prompt: builtin:sre, tools: [\"Bash(y:*)\"]}\n"), "")
	require.NoError(t, err)
	require.Len(t, cfg.Warnings, 1)
	assert.Contains(t, cfg.Warnings[0], "agents.senior-engineer.tools")
}

func TestChiefOfStaffCannotBeDisabledUnderOldName(t *testing.T) {
	_, err := Parse([]byte("agents:\n  orchestrator: {disabled: true}\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents.chief-of-staff.disabled")
}

func TestDogfoodExampleLoadsUnchanged(t *testing.T) {
	t.Setenv("DOGFOOD", t.TempDir())
	cfg, err := Load("../../examples/dogfood/onett.yaml")
	require.NoError(t, err)
	custom := cfg.Agents["implementer-claude"]
	assert.Equal(t, OriginCustom, custom.Origin)
	assert.Equal(t, "builtin:implementer", custom.Prompt, "a prompt reference is kept as written and resolved by the engine")
	assert.Equal(t, "implementer-claude", cfg.Routing["feature"])
}
