package config_test

// A notifier, channel, deployer or observer type that the daemon can run must also have a typed
// options schema, or the web UI could only offer a raw YAML box for it. These tests compare each
// runtime registry with config's option table. They live in an external test package because the
// channel package imports config.

import (
	"testing"

	"github.com/stretchr/testify/assert"

	_ "github.com/bobbyrc/onett/internal/channel/all"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/deploy"
	"github.com/bobbyrc/onett/internal/harness"
	_ "github.com/bobbyrc/onett/internal/harness/claude"
	_ "github.com/bobbyrc/onett/internal/harness/codex"
	_ "github.com/bobbyrc/onett/internal/harness/exec"
	_ "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/observe"
)

func TestOptionSchemasMatchRegistries(t *testing.T) {
	for kind, registered := range map[string][]string{
		"notify":   notify.Types(),
		"channels": channel.Types(),
		"deploy":   deploy.Types(),
		"observe":  observe.Types(),
	} {
		assert.Equal(t, registered, config.OptionTypeNames(kind),
			"%s: the registered types and the typed options in internal/config/options.go differ: add or remove the options struct", kind)
	}
}

func TestHarnessTypesMatchRegistry(t *testing.T) {
	assert.Equal(t, harness.Types(), config.HarnessTypes,
		"config.HarnessTypes must list the registered harness types")
}

func TestSchemaOffersHarnessTypes(t *testing.T) {
	var find func(n config.SchemaNode) *config.SchemaNode
	find = func(n config.SchemaNode) *config.SchemaNode {
		if n.Key == "type" && len(n.Enum) > 0 && assert.ObjectsAreEqual(n.Enum, config.HarnessTypes) {
			return &n
		}
		for _, f := range n.Fields {
			if r := find(f); r != nil {
				return r
			}
		}
		if n.Item != nil {
			return find(*n.Item)
		}
		return nil
	}
	assert.NotNil(t, find(config.Schema()))
}
