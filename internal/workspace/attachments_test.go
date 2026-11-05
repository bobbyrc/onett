package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestCopyAttachmentsIntoIgnoredDir(t *testing.T) {
	repo, m, _ := setup(t)
	ws, err := m.Prepare(context.Background(), repo, "onett/att", "run1")
	require.NoError(t, err)

	src := t.TempDir()
	one := filepath.Join(src, "id1-shot.png")
	two := filepath.Join(src, "id2-shot.png")
	writeFile(t, one, "first")
	writeFile(t, two, "second")
	atts := []domain.Attachment{
		{ID: "id1", Name: "shot.png", MIME: "image/png", Size: 5, Path: one},
		{ID: "id2", Name: "shot.png", MIME: "image/png", Size: 6, Path: two},
		{ID: "id3", Name: "old.log", MIME: "text/plain", Size: 9, Path: filepath.Join(src, "gone")},
		{ID: "id4", Name: "pruned.txt", MIME: "text/plain", Size: 1, Expired: true},
	}

	placed, err := CopyAttachments(ws, atts)
	require.NoError(t, err)
	require.Len(t, placed, 4)

	assert.Equal(t, ".onett-run/attachments/shot.png", placed[0].Rel)
	assert.Equal(t, filepath.Join(ws.Dir, ".onett-run", "attachments", "shot.png"), placed[0].Abs)
	assert.Equal(t, ".onett-run/attachments/id2-shot.png", placed[1].Rel, "a repeated name gets the ID prefix")
	for i, want := range []string{"first", "second"} {
		got, err := os.ReadFile(placed[i].Abs)
		require.NoError(t, err)
		assert.Equal(t, want, string(got))
		st, err := os.Stat(placed[i].Abs)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	}
	assert.True(t, placed[2].Expired, "a file that is gone is skipped")
	assert.True(t, placed[3].Expired)
	assert.Empty(t, placed[2].Rel)

	assert.Empty(t, runGit(t, ws.Dir, "status", "--porcelain"), "attachments are not tracked")

	// A second run's call over the same directory replaces the files rather than failing.
	_, err = CopyAttachments(ws, atts[:1])
	require.NoError(t, err)
}

func TestCopyAttachmentsNone(t *testing.T) {
	placed, err := CopyAttachments(Workspace{Dir: t.TempDir()}, nil)
	require.NoError(t, err)
	assert.Empty(t, placed)
}
