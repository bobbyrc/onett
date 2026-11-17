package engine

import (
	"os"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/redact"
)

// redactSecrets is redact.Secrets: every piece of evidence in this package goes through it (invariant 8).
func redactSecrets(s string, env []string) string { return redact.Secrets(s, env) }

// evidenceEnv returns the environment that the deploy and logs commands of p run with: onett's
// environment, then the project's env. It is the input to redactSecrets for their output.
func evidenceEnv(p *config.Project) []string {
	return append(os.Environ(), projectEnvPairs(p.Env)...)
}
