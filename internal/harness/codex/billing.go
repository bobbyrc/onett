package codex

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/sysproc"
)

const (
	loginStatusTimeout = 10 * time.Second
	billingCacheTTL    = 5 * time.Minute
)

// billingCache remembers the answer of `codex login status` for billingCacheTTL, so a busy
// factory does not spawn it for every run.
type billingCache struct {
	mu      sync.Mutex
	value   string
	checked time.Time
}

// billing returns how runs of this harness are paid for. A config override wins; otherwise it
// asks `codex login status`. Any failure yields "" (unknown) and never fails the run.
func (r *runner) billing(spec harness.Spec) string {
	switch r.cfg.Billing {
	case harness.BillingMetered, harness.BillingSubscription:
		return r.cfg.Billing
	}
	now, probe := r.now, r.loginStatus
	if now == nil {
		now = time.Now
	}
	if probe == nil {
		probe = r.runLoginStatus
	}
	r.cache.mu.Lock()
	defer r.cache.mu.Unlock()
	if !r.cache.checked.IsZero() && now().Sub(r.cache.checked) < billingCacheTTL {
		return r.cache.value
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginStatusTimeout)
	defer cancel()
	out, err := probe(ctx, loginStatusArgs(r.cfg), spec)
	v := ""
	if err == nil {
		v = parseLoginStatus(out)
	}
	r.cache.value, r.cache.checked = v, now()
	return v
}

// loginStatusArgs is the configured codex command followed by `login status`.
func loginStatusArgs(cfg harness.Config) []string {
	args := []string{"codex"}
	if len(cfg.Command) > 0 {
		args = append([]string(nil), cfg.Command...)
	}
	return append(args, "login", "status")
}

// runLoginStatus runs the command with the same environment a run would get and returns its
// combined output.
func (r *runner) runLoginStatus(ctx context.Context, args []string, spec harness.Spec) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = sysproc.AgentEnv(r.cfg.InheritEnv, mergeEnv(r.cfg.Env, spec.Env))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// parseLoginStatus reads `codex login status` output: "Logged in using ChatGPT" is a
// subscription, a login by API key is metered, anything else is unknown.
func parseLoginStatus(out string) string {
	s := strings.ToLower(out)
	switch {
	case strings.Contains(s, "chatgpt"):
		return harness.BillingSubscription
	case strings.Contains(s, "api key"):
		return harness.BillingMetered
	}
	return ""
}
