package observe

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStagingObserverShortensTitleAtWordBoundary runs the dogfood staging observer on a log with
// a long error message: the title must end in an ellipsis at a word boundary, and the full
// message must stay available in the culprit field.
func TestStagingObserverShortensTitleAtWordBoundary(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("/usr/bin/python3 not available")
	}
	msg := "OPERATOR DRILL: synthetic error injected into the staging log to test prod_watch; no code is at fault, close the incident when it appears"
	dir := t.TempDir()
	log := filepath.Join(dir, "staging.log")
	line := `{"time":"2026-01-01T00:00:00Z","level":"ERROR","msg":"` + msg + `"}` + "\n" +
		`{"time":"2026-01-01T00:00:01Z","level":"ERROR","msg":"short one"}` + "\n"
	require.NoError(t, os.WriteFile(log, []byte(line), 0o600))

	cmd := exec.Command("/bin/bash", filepath.Join("..", "..", "examples", "dogfood", "staging", "observe.sh"))
	cmd.Env = append(os.Environ(), "STAGING_OBSERVE_LOG="+log)
	out, err := cmd.Output()
	require.NoError(t, err)

	obs, err := parseObservations(out)
	require.NoError(t, err)
	require.Len(t, obs, 2)

	long := obs[0]
	head := strings.TrimSuffix(long.Title, "…")
	assert.LessOrEqual(t, len([]rune(long.Title)), 120)
	assert.True(t, strings.HasSuffix(long.Title, "…"), long.Title)
	assert.True(t, strings.HasPrefix(msg, head))
	assert.Equal(t, byte(' '), msg[len(head)], "cut at a word boundary")
	assert.Equal(t, msg, long.Culprit)

	assert.Equal(t, "short one", obs[1].Title)
	assert.Empty(t, obs[1].Culprit)
}
