package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Guessed or flag-given setup and checks are always printed before they are written, so the
// owner sees what will run in each task.

func TestAddUseGuessShowsWhatItWrites(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{result: guessedCommands})

	code, out, errOut := runCLI("project", "add", "acme/pimlo", "--use-guess", "--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "\nBefore every task, guessed from package-lock.json and package.json:\n")
	assert.Contains(t, out, "  setup    npm ci\n")
	assert.Contains(t, out, "  checks   npm run lint\n")
	assert.Contains(t, out, "           npm test\n")
	assert.Contains(t, out, "They run in each task's copy with the agent's limited environment, never your credentials.\n")
	assert.Contains(t, out, "Using them (--use-guess).\n")
}

func TestAddFlagsShowWhatTheyWrite(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{})

	code, out, errOut := runCLI("project", "add", "acme/home-lab",
		"--setup", "./scripts/bootstrap.sh", "--check", "make lint", "--check", "make test", "--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "\nBefore every task (from --setup and --check):\n")
	assert.Contains(t, out, "  setup    ./scripts/bootstrap.sh\n")
	assert.Contains(t, out, "  checks   make lint\n")
	assert.Contains(t, out, "           make test\n")
}

func TestAddNoChecksSaysSo(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{})

	code, out, errOut := runCLI("project", "add", "acme/demo", "--no-checks", "--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "\nNo setup or checks (--no-checks): agents get the code with nothing installed, and onett opens pull requests without testing them first.\n")
}
