package engine

import (
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/scm"
)

// commitMessage returns the message for the commit onett makes of an agent's changes in project
// p: the agent's pull request title, or the task title when it gave none. Both can be agent
// text, and a code host can notify a person named in a commit message, so the text goes through
// the project's SCM when that quotes mentions (scm.MentionQuoter). An SCM that cannot be built
// is an error rather than an unquoted message.
func (e *Engine) commitMessage(p *config.Project, prTitle, taskTitle string) (string, error) {
	msg := prTitle
	if msg == "" {
		msg = taskTitle
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return "", err
	}
	if q, ok := s.(scm.MentionQuoter); ok {
		msg = q.QuoteMentions(msg)
	}
	return msg, nil
}
