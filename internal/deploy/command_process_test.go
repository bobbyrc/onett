package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDeployProcessReportsCommandPID(t *testing.T) {
	out := filepath.Join(t.TempDir(), "pid")
	d := newTestCommand(t, map[string]any{
		"deploy": `echo $$ > "$PIDOUT"; echo done`,
	}, map[string]string{"PIDOUT": out})

	pd, ok := d.(ProcessDeployer)
	require.True(t, ok)

	var got int
	_, err := pd.DeployProcess(context.Background(), "abc123", func(pid int) { got = pid })
	require.NoError(t, err)

	b, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(got), strings.TrimSpace(string(b)))
	assert.Greater(t, got, 0)
}
