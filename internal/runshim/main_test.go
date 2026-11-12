package runshim_test

import (
	"os"
	"testing"

	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/runshim/shimtest"
)

// TestMain lets the test binary act as `onett run-shim` when runshim.ReexecEnv is set. The shim
// comes from shimtest, so it stops when the test binary that started it is gone. TestMain is in
// package runshim_test because shimtest imports runshim.
func TestMain(m *testing.M) {
	if os.Getenv(runshim.ReexecEnv) == "1" {
		os.Exit(shimtest.Main())
	}
	os.Exit(m.Run())
}
