package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallTightensAnExistingPlist: the plist can hold secrets, and writing over a file keeps
// its old mode, so a plist that everyone can read is made private by the next install, whether
// or not its content changes.
func TestInstallTightensAnExistingPlist(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outdated bool
	}{
		{name: "same content"},
		{name: "outdated content", outdated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plistDir := stubHost(t)
			dataDir := filepath.Join(t.TempDir(), "data")
			cfg := writeServiceConfig(t, dataDir)
			fakeRunner(t, nil)

			code, _, errOut := runCLI("service", "install", "--config", cfg)
			require.Equal(t, 0, code, errOut)

			plist := filepath.Join(plistDir, "io.onett.serve.plist")
			if tc.outdated {
				require.NoError(t, os.WriteFile(plist, []byte("old"), 0o644))
			}
			require.NoError(t, os.Chmod(plist, 0o644))

			code, _, errOut = runCLI("service", "install", "--config", cfg)
			require.Equal(t, 0, code, errOut)

			fi, err := os.Stat(plist)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
		})
	}
}
