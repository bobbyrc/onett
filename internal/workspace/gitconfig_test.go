package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalConfigKeepsOrdinaryCloneConfig(t *testing.T) {
	repo, _, _ := setup(t)
	dir := t.TempDir()
	c := filepath.Join(dir, "c")
	runGit(t, dir, "clone", repo.Remote, c)
	runGit(t, c, "worktree", "add", "-b", "x", filepath.Join(dir, "wt"), "origin/main")

	data, err := os.ReadFile(filepath.Join(c, ".git", "config"))
	require.NoError(t, err)
	got, err := canonicalConfig(data, "")
	require.NoError(t, err)
	assert.Equal(t, string(data), string(got))
}

func TestCanonicalConfigDropsEverythingNotAllowed(t *testing.T) {
	input := `[core]
	repositoryformatversion = 0
	filemode = true
	bare = true
	fsmonitor = EVIL
	hooksPath = EVIL
	sshCommand = EVIL
	askPass = EVIL
	gitProxy = EVIL
	pager = EVIL
	editor = EVIL
	worktree = EVIL
	attributesFile = EVIL
	alternateRefsCommand = EVIL
[extensions]
	worktreeConfig = true
	partialClone = EVIL
[filter "evil"]
	clean = EVIL
	smudge = EVIL
	process = EVIL
[merge "evil"]
	driver = EVIL
[diff "evil"]
	textconv = EVIL
	command = EVIL
[diff]
	external = EVIL
[gpg]
	program = EVIL
[gpg "ssh"]
	program = EVIL
[commit]
	gpgsign = true
[credential]
	helper = EVIL
[credential "https://github.com"]
	helper = EVIL
[url "https://evil.example/"]
	insteadOf = EVIL
	pushInsteadOf = EVIL
[remote "origin"]
	url = /srv/origin.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	pushurl = EVIL
	uploadpack = EVIL
	receivepack = EVIL
	proxy = EVIL
	vcs = EVIL
	promisor = EVIL
[http]
	proxy = EVIL
	sslVerify = false
	extraHeader = EVIL
[protocol "ext"]
	allow = always
[include]
	path = EVIL
[includeIf "gitdir:/"]
	path = EVIL
[alias]
	st = !sh -c EVIL
[submodule "x"]
	url = EVIL
	update = !EVIL
[branch "b"]
	mergeOptions = EVIL
[user]
	signingKey = EVIL
[sequence]
	editor = EVIL
[fetch]
	recurseSubmodules = EVIL
[init]
	templateDir = EVIL
[branch "main"]
	remote = origin
	merge = refs/heads/main
[user]
	name = Ada Lovelace
	email = ada@example.com
`
	want := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n" +
		"[remote \"origin\"]\n\turl = /srv/origin.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n" +
		"[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n" +
		"[user]\n\tname = Ada Lovelace\n\temail = ada@example.com\n"

	got, err := canonicalConfig([]byte(input), "")
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
	assert.NotContains(t, string(got), "EVIL")
}

func TestCanonicalConfigParserTricks(t *testing.T) {
	const bare = "[core]\n\tbare = false\n"
	cases := []struct {
		name, in, want string
	}{
		{"header and key on one line", "[core] fsmonitor = EVIL\n", bare},
		{"key under unparsable header is not counted under previous section",
			"[core]\n\tfilemode = true\n[evil \"x\\\"y\"]\n\tfilemode = false\n",
			"[core]\n\tfilemode = true\n\tbare = false\n"},
		{"value ending in a backslash is dropped", "[core]\n\tfilemode = true\\\n", bare},
		{"CRLF line ends are accepted", "[core]\r\n\tfilemode = false\r\n",
			"[core]\n\tfilemode = false\n\tbare = false\n"},
		{"valueless key is ignored", "[core]\n\tbare\n", bare},
		{"uppercase names are accepted and written lowercase", "[CORE]\n\tFileMode = false\n",
			"[core]\n\tfilemode = false\n\tbare = false\n"},
		{"remote named like a path is dropped", "[remote \"/tmp/x\"]\n\turl = /y\n", bare},
		{"ext transport url is dropped", "[remote \"origin\"]\n\turl = ext::sh -c EVIL\n", bare},
		{"upload-pack option as url is dropped", "[remote \"origin\"]\n\turl = --upload-pack=EVIL\n", bare},
		{"duplicate fetch line is written once",
			"[remote \"origin\"]\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
			"[core]\n\tbare = false\n[remote \"origin\"]\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"},
		{"quoted value with # is kept and written quoted", "[user]\n\tname = \"Quoted # Name\"\n",
			"[core]\n\tbare = false\n[user]\n\tname = \"Quoted # Name\"\n"},
		{"raw value containing # is dropped", "[user]\n\tname = Bob # comment\n", bare},
		{"branch name with slash is kept", "[branch \"feature/x\"]\n\tremote = origin\n",
			"[core]\n\tbare = false\n[branch \"feature/x\"]\n\tremote = origin\n"},
		{"branch remote that is a URL is dropped", "[branch \"b\"]\n\tremote = https://evil.example/x\n", bare},
		{"unknown repository format version is dropped", "[core]\n\trepositoryformatversion = 7\n", bare},
		{"empty input", "", bare},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalConfig([]byte(tc.in), "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestCanonicalConfigOriginURL(t *testing.T) {
	const bare = "[core]\n\tbare = false\n"
	const fetch = "\tfetch = +refs/heads/*:refs/remotes/origin/*\n"

	got, err := canonicalConfig(nil, "/srv/origin.git")
	require.NoError(t, err)
	assert.Equal(t, bare+"[remote \"origin\"]\n\turl = /srv/origin.git\n"+fetch, string(got))

	in := "[remote \"origin\"]\n\turl = /elsewhere.git\n" + fetch + "\tfetch = +refs/tags/*:refs/tags/*\n"
	got, err = canonicalConfig([]byte(in), "/srv/origin.git")
	require.NoError(t, err)
	assert.Equal(t, bare+"[remote \"origin\"]\n\turl = /srv/origin.git\n"+fetch+
		"\tfetch = +refs/tags/*:refs/tags/*\n", string(got))

	got, err = canonicalConfig(nil, "/path with space/o.git")
	require.NoError(t, err)
	assert.Equal(t, bare+"[remote \"origin\"]\n\turl = /path with space/o.git\n"+fetch, string(got))

	got, err = canonicalConfig(nil, "/odd#name.git")
	require.NoError(t, err)
	assert.Equal(t, bare+"[remote \"origin\"]\n\turl = \"/odd#name.git\"\n"+fetch, string(got))
	again, err := canonicalConfig(got, "")
	require.NoError(t, err)
	assert.Equal(t, string(got), string(again))

	_, err = canonicalConfig(nil, "a\nb")
	assert.Error(t, err)
}

func TestCanonicalConfigIsStable(t *testing.T) {
	cases := []struct {
		in     string
		origin string
	}{
		{"[core]\n\tfilemode = false\n[user]\n\tname = \"Quoted # Name\"\n", ""},
		{"[remote \"origin\"]\n\turl = /elsewhere.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n", "/odd#name.git"},
		{"[branch \"feature/x\"]\n\tremote = origin\n\tmerge = refs/heads/feature/x\n", "/srv/origin.git"},
	}
	for _, tc := range cases {
		first, err := canonicalConfig([]byte(tc.in), tc.origin)
		require.NoError(t, err)
		second, err := canonicalConfig(first, tc.origin)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(second))
	}
}

func TestRepairConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	const original = "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tfsmonitor = EVIL\n" +
		"[remote \"origin\"]\n\turl = /srv/origin.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	const canonical = "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n" +
		"[remote \"origin\"]\n\turl = /srv/origin.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"

	// First repair: the tampered config is replaced, and no copy of the original is kept.
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))
	require.NoError(t, repairConfig(dir, ""))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, canonical, string(got))
	assert.NoFileExists(t, filepath.Join(dir, "config.pre-onett"))
	list := runGit(t, dir, "config", "--file", path, "--list")
	assert.NotContains(t, list, "EVIL")

	// Second repair: tampered again; the config is replaced again and still no backup exists.
	const tampered = "[core]\n\tsymlinks = false\n\tpager = EVIL\n"
	require.NoError(t, os.WriteFile(path, []byte(tampered), 0o644))
	require.NoError(t, repairConfig(dir, ""))
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "[core]\n\tbare = false\n\tsymlinks = false\n", string(got))
	assert.NoFileExists(t, filepath.Join(dir, "config.pre-onett"))

	// Already canonical: nothing is written, so the mtime and the directory do not change.
	require.NoError(t, os.WriteFile(path, []byte(canonical), 0o644))
	info, err := os.Stat(path)
	require.NoError(t, err)
	before, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.NoError(t, repairConfig(dir, ""))
	after, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Equal(t, len(before), len(after))
	info2, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, info.ModTime(), info2.ModTime())
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, canonical, string(got))

	// No temp files are left behind by any of the repairs.
	leftovers, err := filepath.Glob(filepath.Join(dir, "config.onett-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}
