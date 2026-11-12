package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
)

// ghAuthError is what the GitHub CLI prints when it has no login: the line that says what
// failed comes first, and the advice comes last.
const ghAuthError = "gh issue list: exit status 4: To get started with GitHub CLI, please run:  gh auth login\n" +
	"Alternatively, populate the GH_TOKEN environment variable with a GitHub API authentication token.\n"

func TestProblemDetailShowsTheFirstLineAndTheLastTwo(t *testing.T) {
	assert.Equal(t,
		"gh issue list: exit status 4: To get started with GitHub CLI, please run: gh auth login\n"+
			"Alternatively, populate the GH_TOKEN environment variable with a GitHub API authentication token.",
		problemDetail(ghAuthError, nil))
	assert.Equal(t, "one\nfive\nsix", problemDetail("one\n\ntwo\nthree\n  \nfour\nfive\nsix\n", nil))
}

func TestProblemDetailIsBounded(t *testing.T) {
	one := strings.Repeat("l", 1000-len("THE-END")) + "THE-END"
	got := []rune(problemDetail(one, nil))
	assert.Len(t, got, 300)
	assert.True(t, strings.HasPrefix(string(got), "…"))
	assert.True(t, strings.HasSuffix(string(got), "THE-END"))

	three := strings.Repeat("a", 500) + "\n" + strings.Repeat("b", 500) + "\n" + strings.Repeat("c", 500)
	lines := strings.Split(problemDetail(three, nil), "\n")
	require.Len(t, lines, 3)
	for _, l := range lines {
		assert.Len(t, []rune(l), 100)
		assert.True(t, strings.HasPrefix(l, "…"))
	}

	assert.Equal(t, "", problemDetail("", nil))
	assert.Equal(t, "", problemDetail(" \n\t\n  ", nil))
}

func TestProblemDetailRedacts(t *testing.T) {
	env := pbEnv(t)
	t.Setenv("ALPHA_DEPLOY_TOKEN", "alpha-secret-value-1")
	got := problemDetail("deploy failed with alpha-secret-value-1\nretry later", env.e.projectEnv("alpha"))
	assert.NotContains(t, got, "alpha-secret-value-1")
	assert.Contains(t, got, "retry later")
}

func TestFailingLoopDetailStartsWithWhatFailed(t *testing.T) {
	env := pbEnv(t)
	pbLoopError(t, env, loopPRShepherd, "alpha", ghAuthError, env.now.Add(-time.Hour))

	got := pbProblems(t, env, api.ProblemFailingLoops, "alpha")
	require.Len(t, got, 1)
	lines := strings.Split(got[0].Detail, "\n")
	require.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[0], "gh issue list: exit status 4"), lines[0])
}
