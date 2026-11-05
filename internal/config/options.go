package config

// The `options` map of a notifier, channel, deploy block or observer is free-form on disk
// (map[string]any), because each type reads different keys. These structs describe those keys for
// the web UI's editor: Schema() turns each into a section shown when the sibling `type` selects it.
// They are not used to decode the YAML. The runtime still reads the map; a test in this package
// checks that every registered type has an entry here, so a new type cannot ship without one.
//
// A field's yaml tag is the exact option key the runtime reads. Its doc comment becomes the UI
// help, so it states the default. A field tagged `onett:"secret_env"` holds the NAME of an
// environment variable that holds a secret, never the secret itself.

import (
	"reflect"
	"sort"
)

// optionTypes maps a kind (the config list holding the `options` map) and a `type` value to the
// struct that describes that type's options.
var optionTypes = map[string]map[string]reflect.Type{
	"notify": {
		"ntfy":     reflect.TypeOf(NtfyNotifyOptions{}),
		"telegram": reflect.TypeOf(TelegramNotifyOptions{}),
		"discord":  reflect.TypeOf(DiscordNotifyOptions{}),
		"exec":     reflect.TypeOf(ExecNotifyOptions{}),
		"log":      reflect.TypeOf(LogNotifyOptions{}),
	},
	"channels": {
		"telegram": reflect.TypeOf(TelegramChannelOptions{}),
		"discord":  reflect.TypeOf(DiscordChannelOptions{}),
	},
	"deploy": {
		"command": reflect.TypeOf(CommandDeployOptions{}),
		"noop":    reflect.TypeOf(NoopDeployOptions{}),
	},
	"observe": {
		"exec":   reflect.TypeOf(ExecObserveOptions{}),
		"sentry": reflect.TypeOf(SentryObserveOptions{}),
	},
}

// optionKindByStruct names the kind of the config struct whose `options` field is typed.
var optionKindByStruct = map[string]string{
	"Notifier": "notify",
	"Channel":  "channels",
	"Deploy":   "deploy",
	"Observer": "observe",
}

// OptionTypeNames lists the `type` values that have a typed options schema for kind ("notify",
// "channels", "deploy" or "observe"), sorted. Tests compare it with the runtime registries.
func OptionTypeNames(kind string) []string {
	out := make([]string, 0, len(optionTypes[kind]))
	for name := range optionTypes[kind] {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// NtfyNotifyOptions are the options of a `type: ntfy` notifier.
type NtfyNotifyOptions struct {
	// Server is the ntfy server URL. Default https://ntfy.sh.
	Server string `yaml:"server"`
	// Topic is the ntfy topic to publish to. Required.
	Topic string `yaml:"topic"`
	// TokenEnv is the name of an environment variable that holds an ntfy access token, sent as a
	// Bearer token. Optional; without it the notifier posts anonymously.
	TokenEnv string `yaml:"token_env" onett:"secret_env"`
}

// TelegramNotifyOptions are the options of a `type: telegram` notifier (outbound messages only).
type TelegramNotifyOptions struct {
	// BotTokenEnv is the name of an environment variable that holds the Telegram bot token.
	// Required.
	BotTokenEnv string `yaml:"bot_token_env" onett:"secret_env"`
	// ChatID is the chat to send to: a numeric ID or @channelname. Required.
	ChatID string `yaml:"chat_id"`
	// APIURL is the Telegram Bot API base URL. Default https://api.telegram.org.
	APIURL string `yaml:"api_url"`
	// SkipApprovals drops approval requests, for use beside a two-way Telegram channel that
	// already carries them. Default false.
	SkipApprovals bool `yaml:"skip_approvals"`
}

// DiscordNotifyOptions are the options of a `type: discord` notifier (a webhook, outbound only).
type DiscordNotifyOptions struct {
	// WebhookURLEnv is the name of an environment variable that holds the Discord webhook URL,
	// which contains a secret token. Required.
	WebhookURLEnv string `yaml:"webhook_url_env" onett:"secret_env"`
	// Username overrides the webhook's display name. Default: the name set in Discord.
	Username string `yaml:"username"`
}

// ExecNotifyOptions are the options of a `type: exec` notifier.
type ExecNotifyOptions struct {
	// Command is the shell command run for each notification. It receives ONETT_NOTIFY_TITLE,
	// ONETT_NOTIFY_BODY, ONETT_NOTIFY_PRIORITY and ONETT_NOTIFY_URL in its environment. Required.
	Command string `yaml:"command"`
}

// LogNotifyOptions are the options of a `type: log` notifier, which has none.
type LogNotifyOptions struct{}

// TelegramChannelOptions are the options of a `type: telegram` channel (a two-way bot).
type TelegramChannelOptions struct {
	// BotTokenEnv is the name of an environment variable that holds the Telegram bot token.
	// Required.
	BotTokenEnv string `yaml:"bot_token_env" onett:"secret_env"`
	// AllowedUserIDs lists the Telegram user IDs (whole numbers) onett obeys. Required: at least
	// one.
	AllowedUserIDs []int64 `yaml:"allowed_user_ids"`
	// StateFile is the JSON file the bot keeps its state in. Default
	// <data_dir>/channels/telegram.json.
	StateFile string `yaml:"state_file"`
}

// DiscordChannelOptions are the options of a `type: discord` channel (a two-way bot).
type DiscordChannelOptions struct {
	// BotTokenEnv is the name of an environment variable that holds the Discord bot token.
	// Required.
	BotTokenEnv string `yaml:"bot_token_env" onett:"secret_env"`
	// AllowedUserIDs lists the Discord user IDs onett obeys, each written as a quoted string of
	// digits (a 64-bit ID can lose digits as a YAML number). Required: at least one.
	AllowedUserIDs []string `yaml:"allowed_user_ids"`
	// StateFile is the JSON file the bot keeps its state in. Default
	// <data_dir>/channels/discord.json.
	StateFile string `yaml:"state_file"`
}

// CommandDeployOptions are the options of a `type: command` deployer, which runs shell commands.
type CommandDeployOptions struct {
	// Deploy is the shell command that releases the commit in ONETT_REF. Its last output line may
	// be a JSON object {"id","url","state"} or a bare URL. Required.
	Deploy string `yaml:"deploy"`
	// Status is the shell command that reports a release's state, given ONETT_DEPLOY_ID. Default:
	// none; a deploy is then treated as finished as soon as the deploy command exits.
	Status string `yaml:"status"`
	// Rollback is the shell command that restores an earlier release. Default: none, so a
	// rollback cannot be done.
	Rollback string `yaml:"rollback"`
	// Timeout limits each of the three commands. Default 30m.
	Timeout Duration `yaml:"timeout"`
}

// NoopDeployOptions are the options of a `type: noop` deployer, which has none. It is for
// projects released by something else, such as a platform that deploys on push.
type NoopDeployOptions struct{}

// ExecObserveOptions are the options of a `type: exec` observer.
type ExecObserveOptions struct {
	// Command is the shell command that prints the production signals it sees, as a JSON array or
	// JSON Lines. It receives ONETT_SINCE (RFC 3339). Required.
	Command string `yaml:"command"`
	// Timeout limits one run of the command. Default 60s.
	Timeout Duration `yaml:"timeout"`
}

// SentryObserveOptions are the options of a `type: sentry` observer.
type SentryObserveOptions struct {
	// Org is the Sentry organization slug. Required.
	Org string `yaml:"org"`
	// Project is the Sentry project slug. Required.
	Project string `yaml:"project"`
	// BaseURL is the Sentry server URL. Default https://sentry.io.
	BaseURL string `yaml:"base_url"`
	// TokenEnv is the name of an environment variable that holds the Sentry API token. Default
	// SENTRY_AUTH_TOKEN.
	TokenEnv string `yaml:"token_env" onett:"secret_env"`
	// Query is the Sentry issue search to run. Default is:unresolved.
	Query string `yaml:"query"`
}
