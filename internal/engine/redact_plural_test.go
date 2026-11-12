package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecretEnvNamePluralsCount(t *testing.T) {
	for _, name := range []string{
		"DB_PASSWORDS", "SESSION_COOKIES", "REPLICA_DSNS", "GITHUB_PATS", "APIKEYS",
		"SERVICE_PASSWDS", "OAUTH_CREDENTIALS", "API_TOKENS", "SIGNING_KEYS", "APP_SECRETS",
		"db_passwords",
	} {
		assert.True(t, isSecretEnvName(name), name)
	}
	for _, name := range []string{
		"GIT_AUTHOR_NAME", "MONKEYS_COUNT", "COMPASS_HOME", "BYPASS_CACHE",
		"CLAUDE_CODE_SESSION_ID", "PATH", "PASSENGERS",
	} {
		assert.False(t, isSecretEnvName(name), name)
	}
}

func TestRedactSecretsCoversPluralNames(t *testing.T) {
	got := redactSecrets("login with hunter2secret failed", []string{"DB_PASSWORDS=hunter2secret"})
	assert.Equal(t, "login with *** failed", got)
}
