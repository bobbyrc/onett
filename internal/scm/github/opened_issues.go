package github

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/scm"
)

// openedIssueJSON is one element of GitHub's REST issue listing.
type openedIssueJSON struct {
	Number            int        `json:"number"`
	HTMLURL           string     `json:"html_url"`
	Title             string     `json:"title"`
	Body              string     `json:"body"` // null for an empty body, which decodes as ""
	State             string     `json:"state"`
	Labels            []nameJSON `json:"labels"`
	User              *loginJSON `json:"user"`
	AuthorAssociation string     `json:"author_association"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	// PullRequest is present only when the element is a pull request.
	PullRequest *json.RawMessage `json:"pull_request"`
}

// ListOpenedIssues lists the open issues created after since, oldest first, with each author's
// association. It uses the REST listing because `gh issue list --json` has no author
// association for the issue itself. That listing also returns pull requests, which are skipped.
func (g *GitHub) ListOpenedIssues(ctx context.Context, since time.Time) ([]scm.Issue, error) {
	out, err := g.run(ctx, "", "api", "--method", "GET", "repos/"+g.Repo+"/issues",
		"-f", "state=open", "-f", "sort=created", "-f", "direction=desc", "-f", "per_page=100")
	if err != nil {
		return nil, fmt.Errorf("list opened issues: %w", err)
	}
	issues := []scm.Issue{}
	if len(strings.TrimSpace(string(out))) == 0 {
		return issues, nil
	}
	var raw []openedIssueJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode opened issues: %w", err)
	}
	for _, r := range raw {
		if r.PullRequest != nil || !r.CreatedAt.After(since) {
			continue
		}
		is := scm.Issue{
			Number:            r.Number,
			URL:               r.HTMLURL,
			Title:             r.Title,
			Body:              r.Body,
			State:             strings.ToLower(r.State),
			AuthorAssociation: r.AuthorAssociation,
			CreatedAt:         r.CreatedAt,
			UpdatedAt:         r.UpdatedAt,
		}
		if r.User != nil {
			is.Author = r.User.Login
		}
		for _, l := range r.Labels {
			is.Labels = append(is.Labels, l.Name)
		}
		issues = append(issues, is)
	}
	sort.Slice(issues, func(i, j int) bool {
		if !issues[i].CreatedAt.Equal(issues[j].CreatedAt) {
			return issues[i].CreatedAt.Before(issues[j].CreatedAt)
		}
		return issues[i].Number < issues[j].Number
	})
	return issues, nil
}
