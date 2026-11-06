package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishAssetsPushesOnlySafePNGs(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/shots", "run1")
	require.NoError(t, err)

	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a-desktop.png"), "png-a")
	writeFile(t, filepath.Join(src, "notes.txt"), "not an image")
	writeFile(t, filepath.Join(src, "bad name.png"), "spaces")
	secret := filepath.Join(t.TempDir(), "secret")
	writeFile(t, secret, "secret")
	require.NoError(t, os.Symlink(secret, filepath.Join(src, "link.png")))

	sha, names, err := m.PublishAssets(ctx, repo, ws, "after", src)
	require.NoError(t, err)
	assert.Equal(t, []string{"a-desktop.png"}, names)

	branch := AssetsBranch("onett/shots", "after")
	assert.Equal(t, sha, runGit(t, repo.Remote, "rev-parse", "refs/heads/"+branch))
	assert.Equal(t, "png-a", runGit(t, repo.Remote, "show", sha+":a-desktop.png"))

	// A second publish builds on the first, so the old commit stays reachable.
	writeFile(t, filepath.Join(src, "a-desktop.png"), "png-a2")
	sha2, _, err := m.PublishAssets(ctx, repo, ws, "after", src)
	require.NoError(t, err)
	assert.NotEqual(t, sha, sha2)
	assert.Equal(t, sha, runGit(t, repo.Remote, "rev-parse", sha2+"~1"))
}

func TestPublishAssetsNothingToPublish(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "onett/none", "run1")
	require.NoError(t, err)

	sha, names, err := m.PublishAssets(ctx, repo, ws, "after", filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	assert.Empty(t, sha)
	assert.Empty(t, names)

	_, _, err = m.PublishAssets(ctx, repo, ws, "../x", t.TempDir())
	assert.Error(t, err)
}
