package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaChannelOptionsAreAVariant(t *testing.T) {
	channels := schemaField(t, Schema().Fields, "channels")
	typ := schemaField(t, channels.Item.Fields, "type")
	assert.Equal(t, []string{"discord", "telegram"}, typ.Enum)

	options := schemaField(t, channels.Item.Fields, "options")
	assert.Equal(t, "variant", options.Type)
	assert.Equal(t, "type", options.Discriminator)
	require.Contains(t, options.Variants, "telegram")

	tg := options.Variants["telegram"]
	assert.Equal(t, "section", tg.Type)
	token := schemaField(t, tg.Fields, "bot_token_env")
	assert.True(t, token.SecretEnv)
	assert.Contains(t, token.Help, "environment variable")
	ids := schemaField(t, tg.Fields, "allowed_user_ids")
	assert.Equal(t, "list", ids.Type)
	assert.Equal(t, "int", ids.Item.Type)
	state := schemaField(t, tg.Fields, "state_file")
	assert.False(t, state.SecretEnv)
	assert.Contains(t, state.Help, "Default")

	dids := schemaField(t, options.Variants["discord"].Fields, "allowed_user_ids")
	assert.Equal(t, "string", dids.Item.Type)
}

func TestSchemaDeployOptionsAreAVariant(t *testing.T) {
	projects := schemaField(t, Schema().Fields, "projects")
	deploy := schemaField(t, projects.Item.Fields, "deploy")
	assert.Equal(t, []string{"command", "noop"}, schemaField(t, deploy.Fields, "type").Enum)
	options := schemaField(t, deploy.Fields, "options")
	assert.Equal(t, "variant", options.Type)
	assert.Equal(t, "duration", schemaField(t, options.Variants["command"].Fields, "timeout").Type)
	assert.Empty(t, options.Variants["noop"].Fields)
}

func TestSchemaSecretEnvFieldsAreTheEnvNameOptions(t *testing.T) {
	var got []string
	for kind, types := range optionTypes {
		for name, typ := range types {
			for i := range typ.NumField() {
				if typ.Field(i).Tag.Get("onett") == "secret_env" {
					got = append(got, kind+"."+name+"."+typ.Field(i).Tag.Get("yaml"))
				}
			}
		}
	}
	assert.ElementsMatch(t, []string{
		"notify.ntfy.token_env", "notify.telegram.bot_token_env", "notify.discord.webhook_url_env",
		"channels.telegram.bot_token_env", "channels.discord.bot_token_env", "observe.sentry.token_env",
	}, got)
}
