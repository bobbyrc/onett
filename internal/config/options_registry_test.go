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
