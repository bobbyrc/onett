package config

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaAt walks the schema by YAML keys; "*" steps into a map's or list's item.
func schemaAt(t *testing.T, keys ...string) SchemaNode {
	t.Helper()
	n := Schema()
	for _, k := range keys {
		var next *SchemaNode
		if k == "*" {
			next = n.Item
		} else {
			for i := range n.Fields {
				if n.Fields[i].Key == k {
					next = &n.Fields[i]
				}
			}
		}
		require.NotNil(t, next, "no schema node %v", keys)
		n = *next
	}
	return n
}

func TestSchemaDefaultsComeFromApplyDefaults(t *testing.T) {
	var c Config
	ApplyDefaults(&c)

	stall := schemaAt(t, "limits", "stall_timeout")
	assert.Equal(t, shortDuration(c.Limits.StallTimeout.Duration), stall.Default)
	assert.Equal(t, "15m", stall.Default)

	goalTurns := schemaAt(t, "limits", "max_goal_turns")
	assert.EqualValues(t, c.Limits.MaxGoalTurns, goalTurns.Default)

	// Inline and nested loop fields.
	assert.Equal(t, shortDuration(c.Loops.IssueIntake.Every.Duration), schemaAt(t, "loops", "issue_intake", "every").Default)
	assert.Equal(t, c.Loops.Friction.Label, schemaAt(t, "loops", "friction", "label").Default)

	// Entries of a map get the defaults applied to every entry.
	assert.Equal(t, "main", schemaAt(t, "projects", "*", "default_branch").Default)
	assert.Equal(t, "default", schemaAt(t, "projects", "*", "policy").Default)
	assert.EqualValues(t, DefaultPolicy().MaxLines, schemaAt(t, "policies", "*", "max_lines").Default)
	assert.Equal(t, "10m", schemaAt(t, "projects", "*", "deploy", "verify_for").Default)
}

func TestSchemaDefaultsSkipContextAndZero(t *testing.T) {
	// The path depends on the project's name; an agent's harness on the agent.
	assert.Nil(t, schemaAt(t, "projects", "*", "path").Default)
	assert.Nil(t, schemaAt(t, "agents", "*", "harness").Default)
	// No default to show for a setting that is empty unless set.
	assert.Nil(t, schemaAt(t, "server", "allowed_hosts").Default)
}

func TestShortDuration(t *testing.T) {
	for in, want := range map[string]string{"15m0s": "15m", "2h0m0s": "2h", "1h30m0s": "1h30m", "30s": "30s", "720h0m0s": "720h"} {
		d, err := time.ParseDuration(in)
		require.NoError(t, err)
		assert.Equal(t, want, shortDuration(d), in)
	}
}

// schemaFixturePath is the real schema as JSON, for the web UI's tests: the nav grouping test
// walks it so that a new setting cannot be left out of the nav. Run
// `go test ./internal/config -run TestSchemaFixtureIsCurrent -update` to rewrite it.
const schemaFixturePath = "../../web/src/lib/configSchema.fixture.json"

func TestSchemaFixtureIsCurrent(t *testing.T) {
	want, err := json.MarshalIndent(Schema(), "", "  ")
	require.NoError(t, err)
	want = append(want, '\n')
	if *updateCoverageDoc {
		require.NoError(t, os.WriteFile(schemaFixturePath, want, 0o644))
		return
	}
	got, err := os.ReadFile(schemaFixturePath)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "web schema fixture is stale: run go test ./internal/config -run TestSchemaFixtureIsCurrent -update")
}
