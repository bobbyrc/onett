package github

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openedIssuesJSON is the REST listing as GitHub returns it: newest first, with one pull request.
const openedIssuesJSON = `[
 {"number": 14, "html_url": "https://github.com/o/r/issues/14", "title": "Outside report", "body": null, "state": "open", "labels": [], "user": {"login": "stranger"}, "author_association": "NONE", "created_at": "2026-11-12T10:05:00Z", "updated_at": "2026-11-12T10:06:00Z"},
 {"number": 13, "html_url": "https://github.com/o/r/pull/13", "title": "A pull request", "body": "x", "state": "open", "labels": [], "user": {"login": "alice"}, "author_association": "OWNER", "created_at": "2026-11-12T10:03:00Z", "updated_at": "2026-11-12T10:03:00Z", "pull_request": {"url": "https://api.github.com/repos/o/r/pulls/13"}},
 {"number": 12, "html_url": "https://github.com/o/r/issues/12", "title": "Crash on start", "body": "It crashes.", "state": "open", "labels": [{"name": "bug"}, {"name": "p1"}], "user": {"login": "alice"}, "author_association": "OWNER", "created_at": "2026-11-12T10:01:00Z", "updated_at": "2026-11-12T10:02:00Z"},
 {"number": 9, "html_url": "https://github.com/o/r/issues/9", "title": "Old one", "body": "old", "state": "open", "labels": [], "user": {"login": "alice"}, "author_association": "OWNER", "created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z"}
]`

func TestListOpenedIssuesSkipsPullRequestsAndOldIssues(t *testing.T) {
	f := newFake()
	f.on["api"] = openedIssuesJSON
	g := NewWithRunner("o/r", f.runner())
	since := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

	got, err := g.ListOpenedIssues(context.Background(), since)
	require.NoError(t, err)
	require.Len(t, got, 2)

	first := got[0]
	assert.Equal(t, 12, first.Number)
	assert.Equal(t, "https://github.com/o/r/issues/12", first.URL)
	assert.Equal(t, "Crash on start", first.Title)
	assert.Equal(t, "It crashes.", first.Body)
	assert.Equal(t, "open", first.State)
	assert.Equal(t, []string{"bug", "p1"}, first.Labels)
	assert.Equal(t, "alice", first.Author)
	assert.Equal(t, "OWNER", first.AuthorAssociation)
	assert.True(t, first.CreatedAt.Equal(time.Date(2026, 11, 12, 10, 1, 0, 0, time.UTC)))
	assert.Nil(t, first.Comments)

	second := got[1]
	assert.Equal(t, 14, second.Number)
	assert.Equal(t, "", second.Body, "a null body decodes as empty")
	assert.Equal(t, "stranger", second.Author)
	assert.Equal(t, "NONE", second.AuthorAssociation)
}

func TestListOpenedIssuesAsksForNewestOpenIssues(t *testing.T) {
	f := newFake()
	f.on["api"] = "[]"
	g := NewWithRunner("o/r", f.runner())

	_, err := g.ListOpenedIssues(context.Background(), time.Time{})
	require.NoError(t, err)
	require.Len(t, f.calls, 1)
	assert.Equal(t, []string{"api", "--method", "GET", "repos/o/r/issues", "-f", "state=open",
		"-f", "sort=created", "-f", "direction=desc", "-f", "per_page=100"}, f.calls[0].args)
}

func TestListOpenedIssuesIssueCreatedExactlyAtSinceIsNotNew(t *testing.T) {
	f := newFake()
	f.on["api"] = openedIssuesJSON
	g := NewWithRunner("o/r", f.runner())
	since := time.Date(2026, 11, 12, 10, 1, 0, 0, time.UTC)

	got, err := g.ListOpenedIssues(context.Background(), since)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 14, got[0].Number)
}

func TestListOpenedIssuesNoneIsEmptyNotNil(t *testing.T) {
	f := newFake()
	f.on["api"] = "[]"
	g := NewWithRunner("o/r", f.runner())

	got, err := g.ListOpenedIssues(context.Background(), time.Time{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got, 0)
}

func TestListOpenedIssuesEmptyOutputIsNoIssues(t *testing.T) {
	f := newFake()
	g := NewWithRunner("o/r", f.runner())

	got, err := g.ListOpenedIssues(context.Background(), time.Time{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got, 0)
}

func TestListOpenedIssuesReportsGhFailure(t *testing.T) {
	f := newFake()
	f.errOn["api"] = errors.New("boom")
	g := NewWithRunner("o/r", f.runner())

	_, err := g.ListOpenedIssues(context.Background(), time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list opened issues")
	assert.Contains(t, err.Error(), "boom")
}

func TestListOpenedIssuesReportsBadJSON(t *testing.T) {
	f := newFake()
	f.on["api"] = "{"
	g := NewWithRunner("o/r", f.runner())

	_, err := g.ListOpenedIssues(context.Background(), time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode opened issues")
}
