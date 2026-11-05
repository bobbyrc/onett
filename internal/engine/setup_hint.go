package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/config"
)

// setupBackoff is how long a transient setup failure waits before the task is re-queued, so a
// registry outage is not retried in a tight loop that burns every attempt. A variable so tests
// can shorten it.
var setupBackoff = time.Minute

// transientRE matches setup output that points at the network or a package registry rather
// than at the tree: a retry later may pass.
var transientRE = regexp.MustCompile(`(?i)ETIMEDOUT|ECONNRESET|ECONNREFUSED|ENOTFOUND|EAI_AGAIN|network|` +
	`timed? ?out|temporary failure|could not resolve|connection (reset|refused|timed)|` +
	`TLS handshake|\b50[234]\b|registry`)

// transientFailure reports whether a setup command's output looks like a network or registry
// problem. Anything else is treated as deterministic for the current tree.
func transientFailure(output string) bool { return transientRE.MatchString(output) }

// setupCommandList renders the setup commands as a bullet list.
func setupCommandList(cmds []config.Check) string {
	var b strings.Builder
	for _, c := range cmds {
		fmt.Fprintf(&b, "- %s: `%s`\n", c.Name, c.Run)
	}
	return b.String()
}

// setupDeferredSection tells the agent onett skipped setup because the tree has merge conflicts.
func setupDeferredSection(cmds []config.Check) string {
	return "\n\n## Project setup was skipped\n\nThe tree has unresolved merge conflicts, so onett did not run the project's setup " +
		"(it would fail on conflict markers). Resolve the conflicts first (the listed files), then run the setup commands yourself:\n" +
		setupCommandList(cmds) + "\nOnett runs setup again when you finish; if it still fails, the task is blocked.\n"
}

// setupFailedSection hands the agent a failed setup command as the first thing to fix.
func setupFailedSection(f *checkFailure) string {
	return fmt.Sprintf("\n\n## Project setup failed: fix this first\n\nOnett ran the project's setup command %q before you started "+
		"and it failed. Fix the cause in the tree (a broken manifest or lockfile, for example), then run the command yourself to confirm. "+
		"Onett runs setup again when you finish; if it still fails, the task is blocked.\n\n%s\n", f.name, f.context())
}
