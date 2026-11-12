package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/scm"
)

// titleArg returns the value that follows the --title flag in a recorded gh call.
func titleArg(t *testing.T, args []string) string {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--title" {
			return args[i+1]
		}
	}
	t.Fatalf("no --title in %v", args)
	return ""
}

func TestOpenPRQuotesMentionsInTheTitle(t *testing.T) {
	f := newFake()
	f.on["pr create"] = "https://github.com/owner/name/pull/5"
	f.on["pr view"] = `{"number":5,"state":"OPEN","headRefName":"b","statusCheckRollup":[]}`
	g := NewWithRunner(repo, f.runner())

	_, err := g.OpenPR(context.Background(), scm.PRSpec{
		Head: "b", Base: "main", Title: "Fix the crash @alice reported",
	})
	require.NoError(t, err)
	require.NotEmpty(t, f.calls)
	assert.Equal(t, "Fix the crash @​alice reported", titleArg(t, f.calls[0].args))
}

func TestCreateIssueQuotesMentionsInTheTitle(t *testing.T) {
	f := newFake()
	f.on["issue create"] = "https://github.com/owner/name/issues/9\n"
	g := NewWithRunner(repo, f.runner())

	issue, err := g.CreateIssue(context.Background(), scm.IssueSpec{
		Title: "Ask @org/team about flaky tests", Body: "details",
	})
	require.NoError(t, err)

	var createArgs []string
	for _, c := range f.calls {
		if len(c.args) > 1 && c.args[0] == "issue" && c.args[1] == "create" {
			createArgs = c.args
		}
	}
	require.NotNil(t, createArgs)
	assert.Equal(t, "Ask @​org/team about flaky tests", titleArg(t, createArgs))
	assert.Equal(t, "Ask @org/team about flaky tests", issue.Title)
}

func TestIssueTitlesReadBackCompareEqualToWhatWasPosted(t *testing.T) {
	f := newFake()
	f.on["issue list"] = `[{"number":3,"url":"u","title":"` + quoteMentions("Ask @bob about it") +
		`","body":"","state":"OPEN","labels":[]}]`
	g := NewWithRunner(repo, f.runner())

	issues, err := g.ListIssues(context.Background(), "", "open")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "Ask @bob about it", issues[0].Title)
}

func TestUnquoteMentions(t *testing.T) {
	for _, s := range []string{
		"no mention",
		"@bob at start",
		"cc @alice and @org/team",
		"mail a@b.com",
	} {
		assert.Equal(t, s, unquoteMentions(quoteMentions(s)), "input %q", s)
	}
}
