package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/workspace"
)

func TestPRBodyPutsShotsBeforeFooter(t *testing.T) {
	got := prBody(domain.Task{ID: "t1", Role: "senior-engineer"},
		domain.RunResult{PRBody: "Did it."}, "\n\n### Screenshots: after\n\n![a](u)\n")
	assert.Contains(t, got, "Did it.\n\n### Screenshots: after\n\n![a](u)\n")
	assert.Less(t, indexOf(got, "![a](u)"), indexOf(got, "Opened by onett"))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestShotsSectionSkipsNonGitHubRemote(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, domain.OutboxDir, domain.ShotsDir, "after"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, domain.OutboxDir, domain.ShotsDir, "after", "a.png"), []byte("x"), 0o644))
	e := &Engine{}
	got := e.shotsSection(context.Background(), domain.Task{}, &config.Project{Repo: "/srv/git/x.git"},
		workspace.Workspace{Dir: dir})
	assert.Empty(t, got)
}
