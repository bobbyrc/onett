package workspace

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// No test may reach a real host: git refuses every remote that is not a local path, so a
	// test that names a GitHub project fails at once instead of calling GitHub. A test that needs
	// its own local http or ssh stub widens this for itself with t.Setenv.
	os.Setenv("GIT_ALLOW_PROTOCOL", "file")
	os.Exit(m.Run())
}
