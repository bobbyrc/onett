package github

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/scm"
)

func TestGitHubQuotesMentionsForTheEngine(t *testing.T) {
	var q scm.MentionQuoter = &GitHub{}
	require.Equal(t, "Fix the crash @​alice reported", q.QuoteMentions("Fix the crash @alice reported"))
	require.Equal(t, "no mention here", q.QuoteMentions("no mention here"))
}
