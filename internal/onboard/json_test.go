package onboard

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A failed copy must still marshal its guess lists as [], not null: the UI maps over them.
func TestInspectFailedCopyJSONHasArrays(t *testing.T) {
	svc, dataDir := newTestService(t, &fakeGH{})
	res, err := svc.Inspect(context.Background(), Request{
		Repo: filepath.Join(t.TempDir(), "missing.git"), DataDir: dataDir,
	}, nil)
	require.NoError(t, err)
	require.False(t, res.Copy.OK)

	raw, err := json.Marshal(res)
	require.NoError(t, err)
	s := string(raw)
	assert.Contains(t, s, `"setup":[]`)
	assert.Contains(t, s, `"checks":[]`)
	assert.Contains(t, s, `"top_files":[]`)
	assert.False(t, strings.Contains(s, "null"), "no null in the JSON: %s", s)
}
