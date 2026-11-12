package engine

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
)

// A secret that straddles the start of the kept tail must not leave its second half behind.
func TestRunCommandsRedactsSecretCutByTruncation(t *testing.T) {
	const secret = "tok-1234567890abcdef" // 20 bytes
	// 9000 bytes, then the secret, then 8182 bytes: the last 8192 bytes of the raw output start
	// with the secret's last 10 bytes.
	run := `head -c 9000 /dev/zero | tr '\0' 'a'; printf '%s' "$API_TOKEN"; head -c 8182 /dev/zero | tr '\0' 'b'; exit 3`
	f := runCommands(context.Background(), []config.Check{{Name: "big", Run: run}}, t.TempDir(),
		[]string{"PATH=" + os.Getenv("PATH"), "API_TOKEN=" + secret})
	require.NotNil(t, f)
	assert.NotContains(t, f.output, "abcdef", "the second half of a secret cut by truncation leaked")
	assert.Contains(t, f.output, "***")
	assert.LessOrEqual(t, len(f.output), checkOutputTail)
}
