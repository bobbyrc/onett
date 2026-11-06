// Package harness runs coding-agent CLIs non-interactively. Each adapter (claude, codex, exec,
// fake) turns one CLI's output into a common stream of Events.
package harness

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// Spec is everything needed to start one agent session.
type Spec struct {
	RunID   string
	Workdir string
	// Prompt is the user turn: the task itself.
	Prompt string
	// Instructions are appended to the agent's system prompt: role, rules, outbox contract.
	Instructions string
	Model        string
	// Effort is the reasoning effort and PermissionMode the permission mode or sandbox, in the
	// adapter's own vocabulary (see Choices). Empty means the adapter's default.
	Effort         string
	PermissionMode string
	// ResumeSessionID continues an earlier session (chat turns, re-runs on the same branch).
	ResumeSessionID string
	// Env is added to the inherited environment.
	Env map[string]string
	// Args are extra adapter-specific CLI arguments from the agent profile.
	Args []string
	// AllowedTools are tool permission rules the run is pre-approved for, in the agent CLI's own
	// syntax (claude-code: "Bash(onett:*)"). Adapters without such rules ignore them.
	AllowedTools []string
	// ConversationID and KeepAlive ask for a long-lived agent process shared by the turns of
	// one conversation. With both set, an adapter that supports it (claude-code) writes Prompt
	// to the conversation's live process instead of starting a new one, and starts one, resuming
	// ResumeSessionID, when there is none. The process is stopped KeepAlive after a turn ends
	// unless another turn starts. The returned Session is one turn: Events closes and Wait
	// returns when the turn's result arrives, not when the process exits. Other adapters ignore
	// both fields and run each Start as its own process.
	ConversationID string
	KeepAlive      time.Duration
}

// Event is one item of a session's output stream.
type Event struct {
	Time time.Time
	Kind domain.LogKind
	Text string
	// ToolID pairs a tool_use event with its tool_result event.
	ToolID string
	// SessionID is set on the first event that reveals it.
	SessionID string
	// Offset is set only for a session behind a run shim, on the last event of each stdout line:
	// the shim's stream offset just after that line. A caller that has stored every event up to
	// this one can persist it and give it back to Reattacher.Reattach.
	Offset int64
	// TurnResult marks the end of one turn of a long-lived process (see Spec.ConversationID):
	// it is set on a final event that carries no log line, and holds that turn's result.
	TurnResult *Result
	// ContextTokens is set on a log-less event (empty Kind) that reports the prompt size of the
	// model request just made, so a caller can show the context filling during the turn rather
	// than only after it. Same meaning as Result.ContextTokens.
	ContextTokens int64
}

// Result is how a session ended.
type Result struct {
	SessionID string
	ExitCode  int
	// FinalText is the agent's last assistant message.
	FinalText   string
	Denials     []string // human-readable description of each refused tool call
	CostUSD     float64
	InputTokens int64
	// CachedInputTokens is the part of InputTokens served from cache (0 if not reported).
	CachedInputTokens int64
	// CacheWriteTokens is the part of InputTokens written to the prompt cache (0 if not
	// reported). Input that is neither read nor written was not cached at all.
	CacheWriteTokens int64
	OutputTokens     int64
	// ContextTokens is the size of the last model request's prompt, which is how full the context
	// window is now (0 if not reported). InputTokens sums every request and cannot say that.
	ContextTokens int64
	// ContextWindow is the model's context window in tokens as the CLI reported it (0 if not
	// reported). Aliases such as "opus" hide whether it is 200k or 1M, so it is never guessed.
	ContextWindow int64
	IsError       bool
	// Billing says how the run was paid for: BillingMetered, BillingSubscription, or "" when
	// the adapter cannot tell.
	Billing string
	// RateLimit is set only when the run stopped because the harness's usage limit was hit.
	// A limit warning during a run that completes normally does not set it.
	RateLimit *RateLimit
}

// Billing values for Result.Billing. A subscription run's cost is an estimate at API prices that
// nobody is billed for; a metered run is pay-per-token.
const (
	BillingMetered      = "metered"
	BillingSubscription = "subscription"
)

// Session is a running agent process.
type Session interface {
	// Events is closed when the process has exited and all output is delivered.
	Events() <-chan Event
	// Wait blocks until the process exits and returns the result. Safe to call once Events is
	// drained or concurrently with draining it.
	Wait() (Result, error)
	// Cancel terminates the process (SIGTERM, then SIGKILL after a grace period).
	Cancel()
}

// ErrInputClosed is returned by Steerable.Send once the session has stopped taking input.
var ErrInputClosed = errors.New("harness: session input is closed")

// Steerable is a Session that accepts more user input while it runs.
type Steerable interface {
	// Send writes one user message to the running agent. It returns ErrInputClosed once the
	// session has stopped taking input; the caller then queues the message instead.
	Send(text string) error
}

// Harness starts sessions of one agent CLI.
type Harness interface {
	Start(ctx context.Context, spec Spec) (Session, error)
}

// ErrDetached is returned by Wait on a session that was detached while its agent was still
// running: there is no result, because the run has not ended.
var ErrDetached = errors.New("harness: session detached from a run that is still running")

// Detacher is a Session whose agent can outlive onett (it runs behind a run shim). Detach lets go
// of the agent without stopping it: Events is closed and Wait returns ErrDetached. The agent keeps
// running and Reattacher.Reattach can pick it up again. It reports false, and does nothing, when
// the session cannot outlive onett (a direct child); the caller then cancels it instead.
type Detacher interface {
	Detach() bool
}

// Reattacher is a Harness that can resume a session whose agent is still running behind a run
// shim, started by an earlier process. spec must be the Spec the session was started with, since
// the harness rebuilds its output parser from it. The agent's whole output is replayed to that
// parser, but events for lines before offset from (a value of Event.Offset) are dropped, so each
// event is delivered once across the restart. The error says why the shim could not be reached.
type Reattacher interface {
	Reattach(ctx context.Context, spec Spec, from int64) (Session, error)
}

// LogReplayer is a Harness that can read the output of a run whose shim is gone. ReplayLog parses
// the shim's stream.log for spec (the Spec the session was started with) and returns the events
// for lines from offset from on. The result carries no final Result: the agent never reported one.
type LogReplayer interface {
	ReplayLog(spec Spec, from int64) ([]Event, error)
}

// Factory builds a harness from its config block.
type Factory func(cfg Config) (Harness, error)

// Config is the harness block from onett.yaml.
type Config struct {
	Name    string
	Type    string            // registered factory name
	Command []string          // binary and fixed leading args; adapters have a default
	Env     map[string]string // added to every session
	Args    []string          // added to every session, before Spec.Args
	// InheritEnv names variables passed through from onett's environment on top of the
	// base allowlist (see proc.go). Everything else in onett's environment is withheld.
	InheritEnv []string
	// Billing is the config override ("metered" or "subscription"); empty or "auto" means the
	// adapter detects it.
	Billing string
	// Isolate runs the agent without the owner's personal setup (claude-code: no MCP servers
	// from user config, no user-level settings, hooks or plugins). Adapters that have no such
	// setup ignore it.
	Isolate bool
	// FullAccess runs the agent with no sandbox and no permission prompts, for adapters that
	// have such a mode (claude-code, codex). It replaces only the adapter's built-in default:
	// a mode chosen in the Spec or a flag in Args wins.
	FullAccess bool
	// Shim, when set, runs the harness's processes behind a per-run shim (limits.run_shim).
	Shim *ShimOptions
}

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes a harness type available to config. Adapters call it from init.
func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = f
}

// New builds a harness of cfg.Type.
func New(cfg Config) (Harness, error) {
	mu.RLock()
	f, ok := factories[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("harness %q: unknown type %q (known: %v)", cfg.Name, cfg.Type, Types())
	}
	return f(cfg)
}

// Types lists registered harness types.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for t := range factories {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
