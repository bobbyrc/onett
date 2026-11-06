// Package claude runs Claude Code non-interactively (claude -p with stream-json input and
// output) as a onett harness of type "claude-code".
package claude

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/runshim"
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
	if spec.ConversationID != "" && spec.KeepAlive > 0 {
		return r.startTurn(ctx, spec)
	}
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

// startTurn runs spec as one turn of its conversation's long-lived process; see
// harness.StartConvTurn. The process outlives ctx, which ends only the turn. Behind the run shim
// (limits.run_shim) it also outlives onett, and the next turn after a restart adopts it.
func (r *runner) startTurn(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	cs := harness.ConvStart{
		Fingerprint: r.fingerprint(spec),
		Live:        liveSettings(r.cfg, spec),
		Retune:      func(prev string) ([]byte, bool) { return retuneLines(prev, liveSettings(r.cfg, spec)) },
		Encode:      userLine,
	}
	// procSpec is the long-lived process's spec; each use gets its own parser.
	procSpec := func(ps *parser, proc **harness.Proc) harness.ProcSpec {
		return harness.ProcSpec{
			Command:       buildArgs(r.cfg, spec),
			Dir:           spec.Workdir,
			Env:           mergeEnv(r.cfg.Env, spec.Env),
			InheritEnv:    r.cfg.InheritEnv,
			KeepStdinOpen: true,
			ParseLine:     ps.parseLine,
			// OnExit only runs from Wait, after the process has started.
			OnExit: func(exitCode int, _ error) (harness.Result, error) {
				return ps.result(exitCode, (*proc).StderrTail()), nil
			},
		}
	}
	newPS := func() *parser {
		ps := newParser(spec.Model)
		ps.perTurn = true
		return ps
	}
	shim := r.cfg.Shim
	runID := ""
	if shim != nil {
		runID = harness.ConvRunID(spec.ConversationID)
		cs.RunDir = runshim.RunDir(shim.DataDir, runID)
		cs.Replay = func() ([]harness.Event, error) {
			return harness.ReplayProcessLog(harness.ProcSpec{RunID: runID, Shim: shim, ParseLine: newPS().parseLine}, 0)
		}
		cs.Reattach = func(from int64) (*harness.Proc, error) {
			var proc *harness.Proc
			ps := procSpec(newPS(), &proc)
			ps.RunID, ps.Shim = runID, shim
			p, err := harness.ReattachProcess(context.Background(), ps, from)
			proc = p
			return p, err
		}
	}
	cs.Start = func(first []byte) (*harness.Proc, error) {
		var proc *harness.Proc
		ps := procSpec(newPS(), &proc)
		ps.RunID, ps.Shim, ps.Stdin = runID, shim, string(first)
		p, err := harness.StartProcess(context.Background(), ps)
		proc = p
		return p, err
	}
	return harness.StartConvTurn(ctx, spec, cs)
}

// fingerprint is what a long-lived process is fixed to when it starts: its command line without
// the session to resume (which moves on every turn) or the model and permission mode (which a
// control request changes in place, see liveSettings), and its environment without ONETT_RUN_ID
// (a new run each turn, which a running process cannot be told about).
func (r *runner) fingerprint(spec harness.Spec) string {
	resumeless := spec
	resumeless.ResumeSessionID = ""
	resumeless.Model, resumeless.PermissionMode = "", ""
	env := mergeEnv(r.cfg.Env, spec.Env)
	delete(env, "ONETT_RUN_ID")
	b, _ := json.Marshal(struct {
		Args []string
		Dir  string
		Env  map[string]string // encoding/json sorts map keys
	}{buildArgs(r.cfg, resumeless), spec.Workdir, env})
	return string(b)
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
	if spec.Effort != "" {
		args = append(args, "--effort", spec.Effort)
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
	// One flag per rule, after the extras: repeated --allowedTools flags add up, so an owner's own
	// --allowedTools in the extras cannot drop a rule onett asks for.
	for _, rule := range spec.AllowedTools {
		args = append(args, "--allowedTools", rule)
	}
	// A mode chosen for the conversation comes last so it wins over the profile's own flag.
	switch {
	case spec.PermissionMode != "":
		args = append(args, "--permission-mode", spec.PermissionMode)
	case !hasPermissionFlag(cfg.Args, spec.Args):
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

// liveSettings is the model and permission mode a turn wants, as JSON. They are the settings a
// running claude takes through a control request, so changing them does not replace the process
// and lose its prompt cache. The mode is the one buildArgs would pass, or empty when the extras
// choose it.
func liveSettings(cfg harness.Config, spec harness.Spec) string {
	mode := spec.PermissionMode
	if mode == "" && !hasPermissionFlag(cfg.Args, spec.Args) {
		mode = defaultPermissionMode
	}
	b, _ := json.Marshal(struct{ Model, Mode string }{spec.Model, mode})
	return string(b)
}

// retuneLines returns the control requests that move a process from the settings prev to next
// (both from liveSettings). It reports false when that cannot be done in place: going back to a
// default that was never named (an empty model or mode) has no request.
func retuneLines(prev, next string) ([]byte, bool) {
	var p, n struct{ Model, Mode string }
	if json.Unmarshal([]byte(prev), &p) != nil || json.Unmarshal([]byte(next), &n) != nil {
		return nil, false
	}
	var out []byte
	add := func(subtype, key, value string) {
		b, _ := json.Marshal(struct {
			Type      string            `json:"type"`
			RequestID string            `json:"request_id"`
			Request   map[string]string `json:"request"`
		}{"control_request", "onett-" + subtype, map[string]string{"subtype": subtype, key: value}})
		out = append(append(out, b...), '\n')
	}
	if n.Model != p.Model {
		if n.Model == "" {
			return nil, false
		}
		add("set_model", "model", n.Model)
	}
	if n.Mode != p.Mode {
		if n.Mode == "" {
			return nil, false
		}
		add("set_permission_mode", "mode", n.Mode)
	}
	return out, true
}
