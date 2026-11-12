package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/scm"
	scmfake "github.com/bobbyrc/onett/internal/scm/fake"
)

// quotingSCM is the fake SCM plus scm.MentionQuoter, marking what it was given.
type quotingSCM struct{ *scmfake.Fake }

func (quotingSCM) QuoteMentions(s string) string { return "quoted(" + s + ")" }

func TestCommitMessageIsQuotedByAnSCMThatQuotesMentions(t *testing.T) {
	e := &Engine{d: Deps{SCMFor: func(*config.Project) (scm.SCM, error) { return quotingSCM{scmfake.New()}, nil }}}
	msg, err := e.commitMessage(&config.Project{}, "Fix @alice's bug", "task title")
	require.NoError(t, err)
	require.Equal(t, "quoted(Fix @alice's bug)", msg)
}

func TestCommitMessageIsUnchangedForAnSCMThatDoesNotQuote(t *testing.T) {
	s := scmfake.New()
	_, ok := scm.SCM(s).(scm.MentionQuoter)
	require.False(t, ok)
	e := &Engine{d: Deps{SCMFor: func(*config.Project) (scm.SCM, error) { return s, nil }}}
	msg, err := e.commitMessage(&config.Project{}, "Fix @alice's bug", "task title")
	require.NoError(t, err)
	require.Equal(t, "Fix @alice's bug", msg)
}

func TestCommitMessageFallsBackToTheTaskTitle(t *testing.T) {
	e := &Engine{d: Deps{SCMFor: func(*config.Project) (scm.SCM, error) { return quotingSCM{scmfake.New()}, nil }}}
	msg, err := e.commitMessage(&config.Project{}, "", "task title")
	require.NoError(t, err)
	require.Equal(t, "quoted(task title)", msg)
}

func TestCommitMessageFailsWhenTheSCMCannotBeBuilt(t *testing.T) {
	e := &Engine{d: Deps{SCMFor: func(*config.Project) (scm.SCM, error) { return nil, errors.New("no scm") }}}
	msg, err := e.commitMessage(&config.Project{}, "Fix", "task title")
	require.Error(t, err)
	require.Empty(t, msg)
}
