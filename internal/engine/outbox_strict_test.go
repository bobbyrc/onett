package engine

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// reportPath creates the outbox directory in a new worktree-like temp dir and returns the dir
// and the path of its result.json.
func reportPath(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, domain.OutboxDir), 0o755))
	return dir, filepath.Join(dir, domain.OutboxDir, domain.ResultFile)
}

// TestReportOverTheSizeCapIsRefused: the agent chooses the file's size, so a report over the cap
// is refused rather than read whole.
func TestReportOverTheSizeCapIsRefused(t *testing.T) {
	t.Run("over", func(t *testing.T) {
		dir, path := reportPath(t)
		body := `{"status":"completed","summary":"` + strings.Repeat("a", 1<<20) + `"}`
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		_, ok := parseResult(dir)
		assert.False(t, ok)
	})
	t.Run("under", func(t *testing.T) {
		dir, path := reportPath(t)
		body := `{"status":"completed","summary":"` + strings.Repeat("a", 1000) + `"}`
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		r, ok := parseResult(dir)
		require.True(t, ok)
		assert.Equal(t, domain.ResultCompleted, r.Status)
	})
}

// TestReportThatIsASymlinkIsRefused: a symlink in the report's place is not followed.
func TestReportThatIsASymlinkIsRefused(t *testing.T) {
	dir, path := reportPath(t)
	elsewhere := filepath.Join(dir, "elsewhere.json")
	require.NoError(t, os.WriteFile(elsewhere, []byte(`{"status":"completed","summary":"x"}`), 0o644))
	require.NoError(t, os.Symlink(elsewhere, path))
	_, ok := parseResult(dir)
	assert.False(t, ok)
}

// TestReportThatIsANamedPipeIsRefusedWithoutBlocking: a named pipe at the report path must not
// block the read; with no writer, a plain read would wait for ever.
func TestReportThatIsANamedPipeIsRefusedWithoutBlocking(t *testing.T) {
	dir, path := reportPath(t)
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	done := make(chan bool, 1)
	go func() {
		_, ok := parseResult(dir)
		done <- ok
	}()

	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(5 * time.Second):
		// Unblock the stuck reader so its thread does not stay blocked after the test fails.
		if w, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
		t.Fatal("parseResult blocked on a named pipe")
	}
}

// TestReportWithTrailingDataIsRefused pins the strict document rules: one JSON object and
// nothing else.
func TestReportWithTrailingDataIsRefused(t *testing.T) {
	cases := []string{
		`{"status":"completed","summary":"x"} {"status":"failed","summary":"y"}`,
		`{"status":"completed","summary":"x"}trailing`,
		`null`,
		`[]`,
		`"completed"`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			dir, path := reportPath(t)
			require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
			_, ok := parseResult(dir)
			assert.False(t, ok)
		})
	}
}

// A field onett does not know is ignored, not an error: an unknown field can do nothing, and
// refusing a report for a harmless extra key would throw away a whole run. See parseResult.
func TestReportWithAnUnknownFieldIsAccepted(t *testing.T) {
	dir, path := reportPath(t)
	body := `{"status":"completed","summary":"x","notes":"not in the schema","review":{"approve":true,"confidence":0.9}}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	r, ok := parseResult(dir)
	require.True(t, ok)
	assert.Equal(t, "x", r.Summary)
	require.NotNil(t, r.Review)
	assert.True(t, r.Review.Approve)
}
