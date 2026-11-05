// Package claude runs Claude Code non-interactively (claude -p with stream-json input and
// output) as a onett harness of type "claude-code".
package claude

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/bobbyrc/onett/internal/harness"
)

const harnessType = "claude-code"

// defaultPermissionMode is used when neither the config nor the agent profile chooses one.
// Claude Code's auto mode reviews risky actions; its refusals surface as denials.
const defaultPermissionMode = "auto"

// isolationArgs keep an agent from loading the owner's personal Claude Code setup. Without them
// every session connects the owner's MCP connectors and plugins, whose tools come and go between
// turns: that changes the cached prompt prefix and costs the whole history a cache miss, and it
// adds tens of thousands of tokens of tool definitions. --strict-mcp-config with no --mcp-config
// means no MCP servers; --setting-sources without "user" drops user-level hooks, plugins and
// permissions but keeps the repository's own settings and CLAUDE.md, and leaves the login alone.
var isolationArgs = []string{"--strict-mcp-config", "--setting-sources", "project,local"}

func init() {
	harness.Register(harnessType, func(cfg harness.Config) (harness.Harness, error) {
		return &runner{cfg: cfg}, nil
	})
}

// runner starts Claude Code sessions for one configured harness.
type runner struct {
	cfg harness.Config
}

// session is a running Claude Code process that takes more user messages on stdin.
type session struct {
	*harness.Proc
}

var _ harness.Steerable = (*session)(nil)

// Send writes one user message in stream-json input form. Claude reads it at its next step
// (inside the current turn). The harness closes stdin on every result line, so a Send after
// that returns harness.ErrInputClosed.
func (s *session) Send(text string) error {
	line, err := userLine(text)
	if err != nil {
		return err
	}
	return s.WriteStdin(line)
}

// userLine encodes text as one stream-json input line.
func userLine(text string) ([]byte, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	b, err := json.Marshal(struct {
		Type    string `json:"type"`
		Message msg    `json:"message"`
	}{"user", msg{"user", text}})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Start runs one Claude Code session. The prompt goes to stdin as the first stream-json input
// line, and stdin stays open until the first result line so Send can add messages.
func (r *runner) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	ps, err := r.procSpec(spec)
	if err != nil {
		return nil, err
	}
	p, err := harness.StartProcess(ctx, ps.ProcSpec)
	if err != nil {
		return nil, err
	}
	return ps.attach(p), nil
}

// Reattach resumes the session that Start began for spec, if its shim is still running.
func (r *runner) Reattach(ctx context.Context, spec harness.Spec, from int64) (harness.Session, error) {
	ps, err := r.procSpec(spec)
	if err != nil {
		return nil, err
	}
	p, err := harness.ReattachProcess(ctx, ps.ProcSpec, from)
	if err != nil {
		return nil, err
	}
	return ps.attach(p), nil
}

// ReplayLog returns the output a dead shim's log holds from offset from on. It uses a parser
// without the onResult hook that procSpec installs: that hook waits for attach and closes the
// stdin of a live process, and a replay has neither.
func (r *runner) ReplayLog(spec harness.Spec, from int64) ([]harness.Event, error) {
	ps := newParser(spec.Model)
	return harness.ReplayProcessLog(harness.ProcSpec{
		Command:   buildArgs(r.cfg, spec),
		Dir:       spec.Workdir,
		RunID:     spec.RunID,
		Shim:      r.cfg.Shim,
		ParseLine: ps.parseLine,
	}, from)
}

var (
	_ harness.Reattacher  = (*runner)(nil)
	_ harness.LogReplayer = (*runner)(nil)
)

// claudeProc is the process spec of a session, with the Proc it will run as filled in by attach.
type claudeProc struct {
	harness.ProcSpec
	proc  *harness.Proc
	ready chan struct{}
}

// attach records the started Proc, which the parser and OnExit callbacks use, and wraps it.
func (c *claudeProc) attach(p *harness.Proc) harness.Session {
	c.proc = p
	close(c.ready)
	return &session{Proc: p}
}

// procSpec builds the process spec for spec. The parser it holds is new, so a spec is used for
// one process or reattachment only.
func (r *runner) procSpec(spec harness.Spec) (*claudeProc, error) {
	ps := newParser(spec.Model)
	first, err := userLine(spec.Prompt)
	if err != nil {
		return nil, err
	}
	c := &claudeProc{ready: make(chan struct{})}
	ps.onResult = func() {
		// Claude waits for more input after each result and exits only when stdin closes. The
		// parser goroutine can run before attach has assigned the Proc.
		<-c.ready
		c.proc.CloseStdin()
	}
	c.ProcSpec = harness.ProcSpec{
		Command:       buildArgs(r.cfg, spec),
		Dir:           spec.Workdir,
		Env:           mergeEnv(r.cfg.Env, spec.Env),
		InheritEnv:    r.cfg.InheritEnv,
		RunID:         spec.RunID,
		Shim:          r.cfg.Shim,
		Stdin:         string(first),
		KeepStdinOpen: true,
		ParseLine:     ps.parseLine,
		// OnExit only runs from Wait, after attach.
		OnExit: func(exitCode int, _ error) (harness.Result, error) {
			return ps.result(exitCode, c.proc.StderrTail()), nil
		},
	}
	return c, nil
}

// buildArgs returns the full command line: the binary, the fixed flags, the config and profile
// extras, and the default permission mode when no permission flag was given.
func buildArgs(cfg harness.Config, spec harness.Spec) []string {
	args := []string{"claude"}
	if len(cfg.Command) > 0 {
		args = append([]string(nil), cfg.Command...)
	}
	args = append(args, "-p", "--output-format", "stream-json", "--input-format", "stream-json", "--verbose")
	if spec.Instructions != "" {
		args = append(args, "--append-system-prompt", spec.Instructions)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if spec.ResumeSessionID != "" {
		args = append(args, "--resume", spec.ResumeSessionID)
	}
	if cfg.Isolate {
		// Before the extras, so an owner can still override any of them.
		args = append(args, isolationArgs...)
	}
	args = append(args, cfg.Args...)
	args = append(args, spec.Args...)
	if !hasPermissionFlag(cfg.Args, spec.Args) {
		args = append(args, "--permission-mode", defaultPermissionMode)
	}
	return args
}

// hasPermissionFlag reports whether any extra argument already chooses a permission mode.
func hasPermissionFlag(lists ...[]string) bool {
	for _, list := range lists {
		for _, a := range list {
			if a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") ||
				a == "--dangerously-skip-permissions" {
				return true
			}
		}
	}
	return false
}

// mergeEnv returns the union of the maps, with later maps winning.
func mergeEnv(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
