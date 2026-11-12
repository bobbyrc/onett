package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every setting whose text reaches an agent (its prompt, description and args, a harness's args,
// a project's instructions) refuses a ${NAME} reference, whether or not the variable is set. The
// agent never gets credentials, so expanding the reference would hand it the daemon's environment.
func TestEnvReferenceRefusedInTextGivenToAgents(t *testing.T) {
	cases := []struct {
		name  string
		doc   string
		field string
	}{
		{"agent description", "agents:\n  reviewer:\n    harness: claude\n    description: \"Reviews with ${ONETT_TEST_REF}\"\n", "agents.reviewer.description"},
		{"agent args, block list", "agents:\n  reviewer:\n    harness: claude\n    args:\n      - --api-key\n      - ${ONETT_TEST_REF}\n", "agents.reviewer.args"},
		{"agent args, quoted flow list", "agents:\n  reviewer:\n    harness: claude\n    args: [\"--api-key=${ONETT_TEST_REF}\"]\n", "agents.reviewer.args"},
		{"agent args, unquoted flow list", "agents:\n  reviewer:\n    harness: claude\n    args: [--api-key, ${ONETT_TEST_REF}]\n", "agents.reviewer.args"},
		{"agent args, one reference for the whole list", "agents:\n  reviewer:\n    harness: claude\n    args: ${ONETT_TEST_REF}\n", "agents.reviewer.args"},
		{"agent prompt_text in a flow mapping", "agents: {reviewer: {harness: claude, prompt_text: Use ${ONETT_TEST_REF}}}\n", "agents.reviewer.prompt_text"},
		{"agent description through an alias", "agents:\n  reviewer:\n    harness: claude\n    title: &t \"Uses ${ONETT_TEST_REF}\"\n    description: *t\n", "agents.reviewer.description"},
		{"harness args", "harnesses:\n  claude:\n    type: claude-code\n    args: [\"--settings\", \"${ONETT_TEST_REF}\"]\n", "harnesses.claude.args"},
		{"project instructions", "projects:\n  app:\n    repo: bobbyrc/app\n    instructions: Deploy with ${ONETT_TEST_REF}\n", "projects.app.instructions"},
		{"project instructions, block text", "projects:\n  app:\n    repo: bobbyrc/app\n    instructions: |\n      Deploy with\n      ${ONETT_TEST_REF}\n", "projects.app.instructions"},
		{"project agent description", "projects:\n  app:\n    repo: bobbyrc/app\n    agents:\n      reviewer:\n        description: \"Has ${ONETT_TEST_REF}\"\n", "projects.app.agents.reviewer.description"},
		{"project agent args", "projects:\n  app:\n    repo: bobbyrc/app\n    agents:\n      reviewer:\n        args: [\"${ONETT_TEST_REF}\"]\n", "projects.app.agents.reviewer.args"},
		{"harness command arguments", "harnesses:\n  claude:\n    type: claude-code\n    command: [\"claude\", \"--settings\", \"${ONETT_TEST_REF}\"]\n", "harnesses.claude.command"},
		{"harness command through an alias", "harnesses:\n  mine:\n    type: exec\n    inherit_env: &c [\"/bin/agent\", \"--key=${ONETT_TEST_REF}\"]\n    command: *c\n", "harnesses.mine.command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("unset", func(t *testing.T) {
				t.Setenv("ONETT_TEST_REF", "x")
				require.NoError(t, os.Unsetenv("ONETT_TEST_REF"))
				_, err := Parse([]byte(tc.doc), "")
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.field+": must not contain ${NAME} references")
				assert.ErrorContains(t, err, "which never get credentials")
				assert.ErrorContains(t, err, "inherit_env")
				assert.NotContains(t, err.Error(), "undefined environment variable")
			})

			t.Run("set", func(t *testing.T) {
				t.Setenv("ONETT_TEST_REF", "value-that-must-not-appear")
				_, err := Parse([]byte(tc.doc), "")
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.field+": must not contain ${NAME} references")
				assert.NotContains(t, err.Error(), "value-that-must-not-appear")
			})
		})
	}
}

// The first element of command is the program to run, often a path under a variable; only the
// arguments after it reach the agent's command line as text.
func TestEnvReferenceAllowedInHarnessProgramPath(t *testing.T) {
	t.Setenv("ONETT_TEST_REF", "/opt/agents")
	doc := "harnesses:\n  mine:\n    type: exec\n    command: [\"${ONETT_TEST_REF}/bin/agent\", \"--json\"]\n"
	cfg, err := Parse([]byte(doc), "")
	require.NoError(t, err)
	assert.Equal(t, "/opt/agents/bin/agent", cfg.Harness["mine"].Command[0])
}

// A reference must not reach agent text by standing for a setting's name, a whole section, or
// YAML of its own.
func TestEnvReferenceRefusedHoweverItReachesAgentText(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		doc  string
		want string
	}{
		{
			name: "reference as a setting name",
			env:  map[string]string{"ONETT_TEST_KEY": "prompt_text", "ONETT_TEST_REF": "the-secret-text"},
			doc:  "agents:\n  reviewer:\n    harness: claude\n    ${ONETT_TEST_KEY}: \"Use ${ONETT_TEST_REF}\"\n",
			want: "a ${NAME} reference adds a setting whose text is given to agents",
		},
		{
			name: "two names that are references",
			env:  map[string]string{"ONETT_TEST_P1": "app", "ONETT_TEST_P2": "other", "ONETT_TEST_REF": "the-secret-text"},
			doc:  "projects:\n  ${ONETT_TEST_P1}:\n    repo: bobbyrc/app\n    instructions: \"Use ${ONETT_TEST_REF}\"\n  ${ONETT_TEST_P2}:\n    repo: bobbyrc/other\n",
			want: "a ${NAME} reference adds a setting whose text is given to agents",
		},
		{
			name: "reference that only parses once filled in",
			env:  map[string]string{"ONETT_TEST_ANCHOR": "r", "ONETT_TEST_REF": "the-secret-text"},
			doc:  "projects:\n  app:\n    repo: &r bobbyrc/app\n    instructions: \"Use ${ONETT_TEST_REF}\"\n  other:\n    repo: *${ONETT_TEST_ANCHOR}\n",
			want: "stands where YAML syntax is expected",
		},
		{
			name: "value that holds YAML of its own",
			env:  map[string]string{"ONETT_TEST_PATH": "/srv/x\n    instructions: \"Use the-secret-text\""},
			doc:  "projects:\n  app:\n    repo: bobbyrc/app\n    path: ${ONETT_TEST_PATH}\n",
			want: "a ${NAME} reference adds a setting whose text is given to agents",
		},
		{
			name: "reference for a whole section",
			env:  map[string]string{"ONETT_TEST_AGENTS": "{reviewer: {harness: claude, prompt_text: hello, description: the-secret-text}}"},
			doc:  "agents: ${ONETT_TEST_AGENTS}\n",
			want: "a ${NAME} reference adds a setting whose text is given to agents",
		},
		{
			name: "value that names a new setting",
			env:  map[string]string{"ONETT_TEST_PATH": "/srv/x\n    agents:\n      ghp_SECRETPART: {description: hi}"},
			doc:  "projects:\n  app:\n    repo: bobbyrc/app\n    path: ${ONETT_TEST_PATH}\n",
			want: "a ${NAME} reference adds a setting whose text is given to agents",
		},
		{
			name: "alias that points elsewhere once a retired key is dropped",
			env:  map[string]string{"ONETT_TEST_REF": "the-secret-text"},
			doc:  "projects:\n  app:\n    repo: bobbyrc/app\n    env:\n      TOKEN: &x ${ONETT_TEST_REF}\n    loops:\n      self_update:\n        max_defer: &x safe\n    instructions: *x\n",
			want: "projects.app.instructions: must not contain ${NAME} references",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Parse([]byte(tc.doc), "")
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
			assert.NotContains(t, err.Error(), "the-secret-text")
			assert.NotContains(t, err.Error(), "ghp_SECRETPART")
		})
	}
}

// The words the check uses internally are not special in a config.
func TestMarkerWordsAreOrdinaryText(t *testing.T) {
	doc := "agents:\n  reviewer:\n    harness: claude\n    prompt_text: \"Review the change.\"\n    description: \"the word onettEnvRefA is only a word\"\n"
	_, err := Parse([]byte(doc), "")
	require.NoError(t, err)
}

// Each refused setting is named, so a file with two bad settings reports both.
func TestEnvReferenceRefusalsNameEveryField(t *testing.T) {
	doc := "agents:\n  reviewer:\n    harness: claude\n    description: \"${ONETT_TEST_REF}\"\n" +
		"projects:\n  app:\n    repo: bobbyrc/app\n    instructions: \"${ONETT_TEST_REF}\"\n"
	_, err := Parse([]byte(doc), "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "agents.reviewer.description: must not contain ${NAME} references")
	assert.ErrorContains(t, err, "projects.app.instructions: must not contain ${NAME} references")
}

// A setting in the wrong shape elsewhere in the file does not stop the refusal of a reference.
func TestEnvReferenceRefusedDespiteWrongShapeElsewhere(t *testing.T) {
	t.Setenv("ONETT_TEST_REF", "x")
	doc := "agents:\n  broken: just-a-string\nprojects:\n  app:\n    repo: bobbyrc/app\n" +
		"    instructions: \"Use ${ONETT_TEST_REF}\"\n"
	_, err := Parse([]byte(doc), "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "projects.app.instructions: must not contain ${NAME} references")
}

// A bare $NAME is not a reference, so text given to agents may mention a shell variable as written.
func TestBareDollarNameAcceptedInTextGivenToAgents(t *testing.T) {
	doc := "agents:\n  reviewer:\n    harness: claude\n    prompt_text: \"Review the change.\"\n    description: \"Reads $HOME/notes\"\n" +
		"    args: [\"--note\", \"$HOME\"]\n" +
		"projects:\n  app:\n    repo: bobbyrc/app\n    instructions: \"Scratch files go in $TMPDIR.\"\n"
	cfg, err := Parse([]byte(doc), "")
	require.NoError(t, err)
	assert.Equal(t, "Scratch files go in $TMPDIR.", cfg.Projects["app"].Instructions)
}

// The refusal covers only text given to agents; other settings still expand a reference.
func TestEnvReferenceStillExpandedInOtherSettings(t *testing.T) {
	t.Setenv("ONETT_TEST_REF", "/srv/onett")
	doc := "server:\n  data_dir: ${ONETT_TEST_REF}/data\n" +
		"projects:\n  app:\n    repo: bobbyrc/app\n    path: ${ONETT_TEST_REF}/clone\n" +
		"    env:\n      TOOLS: ${ONETT_TEST_REF}/bin\n"
	cfg, err := Parse([]byte(doc), "")
	require.NoError(t, err)
	assert.Equal(t, "/srv/onett/data", cfg.Server.DataDir)
	assert.Equal(t, "/srv/onett/clone", cfg.Projects["app"].Path)
	assert.Equal(t, "/srv/onett/bin", cfg.Projects["app"].Env["TOOLS"])
}

// An anchor that contains itself must not make the reference check recurse for ever.
func TestEnvReferenceCheckSurvivesAnchorThatContainsItself(t *testing.T) {
	doc := "agents:\n  reviewer:\n    harness: claude\n    args: &a [*a]\n"
	assert.NotPanics(t, func() { _, _ = Parse([]byte(doc), "") })
}
