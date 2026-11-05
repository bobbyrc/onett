package runshim

import (
	"os"
	"testing"
)

// reexecEnv makes the test binary act as `onett run-shim`, so the detached-launch path can be
// tested without a built onett binary.
const reexecEnv = "ONETT_RUNSHIM_REEXEC"

func TestMain(m *testing.M) {
	if os.Getenv(reexecEnv) == "1" {
		os.Exit(Main(os.Args[1:], os.Stderr))
	}
	os.Exit(m.Run())
}
