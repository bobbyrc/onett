package onboard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRepo(t *testing.T) {
	ok := map[string]string{
		"owner/name":                         "owner/name",
		"  owner/name.git  ":                 "owner/name",
		"https://github.com/owner/name":      "owner/name",
		"https://github.com/owner/name.git":  "owner/name",
		"https://github.com/owner/name/":     "owner/name",
		"https://github.com/owner/name.git/": "owner/name",
		"https://www.github.com/owner/name":  "owner/name",
		"git@github.com:owner/name":          "owner/name",
		"git@github.com:owner/name.git":      "owner/name",
		"/srv/git/thing.git":                 "/srv/git/thing.git",
		"/srv/git/x/../thing":                "/srv/git/thing",
	}
	for in, want := range ok {
		got, err := NormalizeRepo(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	bad := []string{
		"foo", "a/b/c", "http://example.com/x", "", "https://github.com/owner",
		"owner/.git", "../x", "/", "https://github.com/owner/name/extra",
	}
	for _, in := range bad {
		_, err := NormalizeRepo(in)
		assert.ErrorIs(t, err, ErrInvalid, in)
	}
}

func TestNormalizeRepoErrorNamesInput(t *testing.T) {
	_, err := NormalizeRepo("foo")
	require.ErrorIs(t, err, ErrInvalid)
	assert.Contains(t, err.Error(), `"foo"`)
}

func TestNameFor(t *testing.T) {
	cases := map[string]string{
		"acme/pimlo":              "pimlo",
		"acme/pimlo.git":          "pimlo",
		"acme/web_app.v2":         "web_app.v2",
		"/srv/git/My Project.git": "My-Project",
		"/srv/git/.hidden":        "hidden",
		"/srv/git/---":            "project",
	}
	for repo, want := range cases {
		assert.Equal(t, want, NameFor(repo), repo)
	}
}

func TestIsGitHub(t *testing.T) {
	assert.True(t, IsGitHub("acme/pimlo"))
	assert.False(t, IsGitHub("/srv/git/pimlo.git"))
}

func TestReadyNeedsPullRequestsForGitHub(t *testing.T) {
	copied := CopyResult{OK: true}
	cases := []struct {
		name string
		res  Result
		want bool
	}{
		{"local copy with branch", Result{Copy: copied, DefaultBranch: "master"}, true},
		{"copy failed", Result{DefaultBranch: "master"}, false},
		{"no branch", Result{Copy: copied}, false},
		{"github without pull requests", Result{Copy: copied, DefaultBranch: "main", GitHub: true}, false},
		{"github with pull requests", Result{Copy: copied, DefaultBranch: "main", GitHub: true,
			PullRequests: PullRequests{OK: true}}, true},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.res.Ready(), c.name)
	}
}

func TestSuggestRepo(t *testing.T) {
	repos := []GitHubRepo{
		{FullName: "acme/pimlo"},
		{FullName: "acme/other"},
	}
	assert.Equal(t, "acme/pimlo", suggestRepo("acme/pimlo-app", repos), "prefix of the requested name")
	assert.Equal(t, "acme/pimlo", suggestRepo("acme/pimlp", repos), "one letter off")
	assert.Equal(t, "", suggestRepo("acme/pimlo", repos), "never the requested repository itself")
	assert.Equal(t, "", suggestRepo("acme/xyzzy", repos), "too far from every repository")
	assert.Equal(t, "acme/pimlo-web", suggestRepo("acme/pimlo", []GitHubRepo{
		{FullName: "acme/pimlo-web"}, {FullName: "acme/pimlo-old"},
	}), "the first in the list wins")
	assert.Equal(t, "", suggestRepo("acme/pimlo", nil))
}
