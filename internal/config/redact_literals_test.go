package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaskLiteralSecrets checks the shape-based masking of credentials typed into commands and
// URLs: the masked rows are secrets, the unchanged rows are shell references and ordinary text.
func TestMaskLiteralSecrets(t *testing.T) {
	cases := []struct{ in, want string }{
		// Masked.
		{`vercel deploy --token abc123`, `vercel deploy --token ***`},
		{`vercel deploy --token=abc123 --prod`, `vercel deploy --token=*** --prod`},
		{`VERCEL_TOKEN=abc123 vercel deploy`, `VERCEL_TOKEN=*** vercel deploy`},
		{`DB_PASSWORD="two words" migrate`, `DB_PASSWORD=*** migrate`},
		{`curl -H "X-Api-Key: abc123" https://api.example`, `curl -H "X-Api-Key: ***" https://api.example`},
		{`curl -H 'Authorization: Bearer abcdefgh12345678' https://api.example`, `curl -H 'Authorization: Bearer ***' https://api.example`},
		{`https://app.example/health?api_key=abc123&verbose=1`, `https://app.example/health?api_key=***&verbose=1`},
		{`psql postgres://app:s3cretpw@db.local/app`, `psql postgres://***@db.local/app`},
		{`git push https://ghp_0123456789abcdefghijABCDEFGHIJ@github.com/o/r`, `git push https://***@github.com/o/r`},
		{`echo ghp_0123456789abcdefghijABCDEFGHIJ | gh auth login --with-token`, `echo *** | gh auth login --with-token`},
		{`deploy --token a1 --api-key b2`, `deploy --token *** --api-key ***`},
		{`psql postgres://app:p@ssword@db.local/app`, `psql postgres://***@db.local/app`},
		{`curl -u admin:s3cretpass https://api.example`, `curl -u *** https://api.example`},
		{`curl --user "admin:s3cretpass" https://api.example`, `curl --user *** https://api.example`},
		{`curl -H 'Authorization: Basic dXNlcjpwYXNzd29yZA==' https://api.example`, `curl -H 'Authorization: Basic ***' https://api.example`},
		{`curl -H "Authorization: abc123secret" https://api.example`, `curl -H "Authorization: ***" https://api.example`},
		{`curl -d '{"api_key":"abc123"}' https://api.example`, `curl -d '{"api_key":***}' https://api.example`},
		{`STRIPE=sk_live_0123456789abcdef run`, `STRIPE=*** run`},
		{`TOKEN='$ecretLiteral' run`, `TOKEN=*** run`},
		{`login --password "$USER-literalpw"`, `login --password ***`},
		{`API_KEY=${API_KEY:-hardcodedsecret} run`, `API_KEY=*** run`},
		{`curl -u "admin:s3cret value" https://api.example`, `curl -u *** https://api.example`},
		{`curl -d hi https://hooks.slack.com/services/T000/B000/XXXXXXXX`, `curl -d hi https://hooks.slack.com/services/***`},
		{`curl -d hi https://discord.com/api/webhooks/123456/abc-DEF_ghi`, `curl -d hi https://discord.com/api/webhooks/***`},
		// Unchanged.
		{`git push -u origin main`, `git push -u origin main`},
		{`TOKEN="${VAULT_TOKEN}" run`, `TOKEN="${VAULT_TOKEN}" run`},
		{`vercel rollback "$ONETT_ROLLBACK_ID" --token "$VERCEL_TOKEN"`, `vercel rollback "$ONETT_ROLLBACK_ID" --token "$VERCEL_TOKEN"`},
		{`TOKEN=$(cat /run/token) ./deploy.sh`, `TOKEN=$(cat /run/token) ./deploy.sh`},
		{`API_KEY=${API_KEY} run`, `API_KEY=${API_KEY} run`},
		{`ssh-keygen -t ed25519 -f id_ed25519`, `ssh-keygen -t ed25519 -f id_ed25519`},
		{`./release.sh --verbose --token --dry-run`, `./release.sh --verbose --token --dry-run`},
		{`scp build.tgz deploy@host.example:/srv/app`, `scp build.tgz deploy@host.example:/srv/app`},
		{`git clone https://github.com/o/r.git`, `git clone https://github.com/o/r.git`},
		{`tail -n 100 /var/log/app.log`, `tail -n 100 /var/log/app.log`},
		{`curl -fsS http://127.0.0.1:8080/health`, `curl -fsS http://127.0.0.1:8080/health`},
		{`***`, `***`},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, maskLiteralSecrets(c.in), "input %q", c.in)
	}
}

// TestRedactedMasksNtfyTopic checks that the ntfy topic, which is the secret on ntfy, is masked
// while the topic of another notifier type is left alone.
func TestRedactedMasksNtfyTopic(t *testing.T) {
	cfg, err := Parse([]byte(`
notify:
  - type: ntfy
    options:
      topic: private-topic-name
      server: "https://user:literal-ntfy-pass@ntfy.example"
  - type: exec
    options:
      topic: not-an-ntfy-topic
      command: "notify.sh --password literal-notify-pass"
`), "/conf/onett.yaml")
	require.NoError(t, err)

	red := cfg.Redacted()

	assert.Equal(t, "***", red.Notify[0].Options["topic"])
	assert.Equal(t, "https://***@ntfy.example", red.Notify[0].Options["server"])
	assert.Equal(t, "not-an-ntfy-topic", red.Notify[1].Options["topic"])
	assert.Equal(t, "notify.sh --password ***", red.Notify[1].Options["command"])

	// The original is untouched.
	assert.Equal(t, "private-topic-name", cfg.Notify[0].Options["topic"])
}

// TestRedactedLiteralMaskingScope checks that the global loops section is scanned for literal
// secrets, while an agent's args are not: the agent editor reads them back from this copy and
// would save the mask.
func TestRedactedLiteralMaskingScope(t *testing.T) {
	cfg, err := Parse([]byte(`
loops:
  self_update:
    command: "UPDATE_SECRET=literal-global-secret ./update.sh"
agents:
  reviewer:
    harness: claude
    prompt_text: "Review the change."
    args: ["--max-tokens", "4096"]
`), "/conf/onett.yaml")
	require.NoError(t, err)

	red := cfg.Redacted()

	assert.Equal(t, "UPDATE_SECRET=*** ./update.sh", red.Loops.SelfUpdate.Command)
	assert.Equal(t, []string{"--max-tokens", "4096"}, red.Agents["code-reviewer"].Args)
}

// TestRedactedMasksHarnessArgumentsButNotTheProgram checks that a value is masked after its flag
// in a harness's args and command. A value is often its own element there, so it is masked too.
// The first element of command is the program path, which GET /agents looks up, so it is kept.
func TestRedactedMasksHarnessArgumentsButNotTheProgram(t *testing.T) {
	cfg, err := Parse([]byte(`
harnesses:
  mine:
    type: exec
    command: ["/opt/sk-learn-tools-for-agents/bin/agent", "--api-key", "literal-cmd-key"]
    args: ["--token", "literal-arg-token", "--verbose", "--password=literal-arg-pass"]
`), "/conf/onett.yaml")
	require.NoError(t, err)

	red := cfg.Redacted()

	assert.Equal(t, []string{"/opt/sk-learn-tools-for-agents/bin/agent", "--api-key", "***"}, red.Harness["mine"].Command)
	assert.Equal(t, []string{"--token", "***", "--verbose", "--password=***"}, red.Harness["mine"].Args)

	// The original is untouched.
	assert.Equal(t, "literal-cmd-key", cfg.Harness["mine"].Command[2])
}

// TestRedactedMasksLiteralSecretsInCommandsAndURLs checks that secrets typed as literals into
// commands, args and URLs are masked wherever the config holds them, and that prose is kept.
func TestRedactedMasksLiteralSecretsInCommandsAndURLs(t *testing.T) {
	cfg, err := Parse([]byte(`
self_project: app
harnesses:
  exec:
    type: exec
    command: ["agent"]
    args: ["--api-key=literal-harness-key"]
projects:
  app:
    repo: bob/app
    path: /tmp/app
    instructions: "Put API_KEY=your-key in .env before you run the tests."
    setup:
      - name: deps
        run: "NPM_TOKEN=literal-npm-token npm ci"
    checks:
      - name: lint
        run: "lint --api-key literal-lint-key ./..."
    intake:
      - name: queue
        command: "fetch https://bot:literal-url-pass@queue.example/items"
    deploy:
      type: command
      health_url: "https://app.example/health?api_key=literal-health-key"
      logs: "tail -n 100 /var/log/app.log"
      options:
        deploy: "vercel deploy --prod --token literal-vercel-token"
        rollback: "vercel rollback \"$ONETT_ROLLBACK_ID\" --token \"$VERCEL_TOKEN\""
    observe:
      - name: errors
        type: exec
        options:
          command: "curl -s -H 'Authorization: Bearer literal-bearer-0001' https://errors.example/api"
    loops:
      self_update:
        enabled: true
        command: "UPDATE_SECRET=literal-update-secret ./update.sh"
        min_healthy: 30m
`), "/conf/onett.yaml")
	require.NoError(t, err)

	red := cfg.Redacted()

	assert.Equal(t, "--api-key=***", red.Harness["exec"].Args[0])
	assert.Equal(t, "NPM_TOKEN=*** npm ci", red.Projects["app"].Setup[0].Run)
	assert.Equal(t, "lint --api-key *** ./...", red.Projects["app"].Checks[0].Run)
	assert.Equal(t, "fetch https://***@queue.example/items", red.Projects["app"].Intake[0].Command)
	assert.Equal(t, "https://app.example/health?api_key=***", red.Projects["app"].Deploy.HealthURL)
	assert.Equal(t, "tail -n 100 /var/log/app.log", red.Projects["app"].Deploy.Logs)
	assert.Equal(t, "vercel deploy --prod --token ***", red.Projects["app"].Deploy.Options["deploy"])
	assert.Equal(t, `vercel rollback "$ONETT_ROLLBACK_ID" --token "$VERCEL_TOKEN"`, red.Projects["app"].Deploy.Options["rollback"])
	assert.Equal(t, "curl -s -H 'Authorization: Bearer ***' https://errors.example/api", red.Projects["app"].Observe[0].Options["command"])
	assert.Equal(t, "UPDATE_SECRET=*** ./update.sh", red.Projects["app"].Loops.SelfUpdate.Command)
	assert.Equal(t, "Put API_KEY=your-key in .env before you run the tests.", red.Projects["app"].Instructions)

	data, err := json.Marshal(red)
	require.NoError(t, err)
	for _, secret := range []string{
		"literal-harness-key", "literal-npm-token", "literal-lint-key", "literal-url-pass",
		"literal-health-key", "literal-vercel-token", "literal-bearer-0001", "literal-update-secret",
	} {
		assert.NotContains(t, string(data), secret)
	}

	// The original is untouched.
	assert.Equal(t, "vercel deploy --prod --token literal-vercel-token", cfg.Projects["app"].Deploy.Options["deploy"])
	assert.Equal(t, "NPM_TOKEN=literal-npm-token npm ci", cfg.Projects["app"].Setup[0].Run)
}
