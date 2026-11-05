package harness

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/runshim"
)

func TestReplayProcessLogReturnsEventsFromOffset(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(runshim.RunDir(dir, "r1"), 0o755))
	// Three complete lines and one the crash cut short.
	log := "a\nb\nc\npartial"
	require.NoError(t, os.WriteFile(runshim.StreamPath(runshim.RunDir(dir, "r1")), []byte(log), 0o644))
	var parsed []string
	spec := ProcSpec{RunID: "r1", Shim: &ShimOptions{DataDir: dir}, ParseLine: func(s string) []Event {
		parsed = append(parsed, s)
		return []Event{{Kind: domain.LogText, Text: s}}
	}}

	evs, err := ReplayProcessLog(spec, 2) // "a\n" is stored already
	require.NoError(t, err)
	require.Len(t, evs, 2)
	assert.Equal(t, "b", evs[0].Text)
	assert.EqualValues(t, 4, evs[0].Offset)
	assert.EqualValues(t, 6, evs[1].Offset)
	assert.Equal(t, []string{"a", "b", "c"}, parsed, "earlier lines rebuild parser state; the torn line is skipped")
	assert.False(t, strings.Contains(strings.Join(parsed, ""), "partial"))

	// No log at all is not an error.
	evs, err = ReplayProcessLog(ProcSpec{RunID: "none", Shim: &ShimOptions{DataDir: dir}, ParseLine: spec.ParseLine}, 0)
	require.NoError(t, err)
	assert.Empty(t, evs)
}
