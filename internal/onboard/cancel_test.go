package onboard

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A check cancelled after it copied a repository removes that copy: the client that started it
// may be gone and has no name to discard the copy with.
func TestInspectCancelledAfterCopyRemovesCopy(t *testing.T) {
	bare := localBareRepo(t)
	svc, dataDir := newTestService(t, &fakeGH{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	copyPath := filepath.Join(dataDir, "repos", "project")

	_, err := svc.Inspect(ctx, Request{Repo: bare, DataDir: dataDir}, func(s Step) {
		if s.Name == "copy" && s.State == "done" {
			cancel()
		}
	})
	require.NoError(t, err)
	assert.NoDirExists(t, copyPath)
	assert.ErrorIs(t, svc.Discard("project", nil), ErrNotFound)
	_, ok := svc.Checked(bare)
	assert.False(t, ok)
}
