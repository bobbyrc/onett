// Package codex runs OpenAI Codex non-interactively (codex exec --json) as an onett harness of
// type "codex".
package codex

import (
	"context"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/harness"
)

const harnessType = "codex"

// defaultSandboxArgs are used when no sandbox or approval flag was given. --approve-for-me alone
// implies the workspace-write sandbox and routes approval requests to automatic review. Codex
// 0.165 rejects it combined with --sandbox, so --sandbox must not be added here.
var defaultSandboxArgs = []string{"--approve-for-me"}

// resumeSandboxOverride is passed as a config override when a session is resumed. `codex exec
// resume` has no --sandbox or --approve-for-me flag, so without this a resumed session runs
// read-only. The value is one argv element; the double quotes are part of it because there is
// no shell.
const resumeSandboxOverride = `sandbox_mode="workspace-write"`

// instructionSeparator joins Spec.Instructions and Spec.Prompt on stdin, because codex has no
// flag for a system prompt.
const instructionSeparator = "\n\n---\n\n"

func init() {
	harness.Register(harnessType, func(cfg harness.Config) (harness.Harness, error) {
		return &runner{cfg: cfg}, nil
	})
}

// runner starts Codex sessions for one configured harness.
type runner struct {
	cfg harness.Config
	// loginStatus runs `codex login status` and now reads the clock; both default when nil and
	// tests replace them.
	loginStatus func(ctx context.Context, args []string, spec harness.Spec) (string, error)
	now         func() time.Time
	cache       billingCache
}

// Start runs one codex exec session, or resumes one. The prompt is written to stdin.
func (r *runner) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	ps, proc := r.procSpec(spec)
	p, err := harness.StartProcess(ctx, ps)
	if err != nil {
		return nil, err
	}
	*proc = p
	return p, nil
}

// Reattach resumes the session that Start began for spec, if its shim is still running.
func (r *runner) Reattach(ctx context.Context, spec harness.Spec, from int64) (harness.Session, error) {
	ps, proc := r.procSpec(spec)
	p, err := harness.ReattachProcess(ctx, ps, from)
	if err != nil {
		return nil, err
	}
	*proc = p
	return p, nil
}

// ReplayLog returns the output a dead shim's log holds from offset from on.
func (r *runner) ReplayLog(spec harness.Spec, from int64) ([]harness.Event, error) {
	ps, _ := r.procSpec(spec)
	return harness.ReplayProcessLog(ps, from)
}

var (
	_ harness.Reattacher  = (*runner)(nil)
	_ harness.LogReplayer = (*runner)(nil)
)

// procSpec builds the process spec for spec, with a new parser, so a spec serves one process or
// reattachment only. OnExit reads the Proc through the returned pointer, which the caller sets
// before Wait can run.
func (r *runner) procSpec(spec harness.Spec) (harness.ProcSpec, **harness.Proc) {
	ps := newParser()
	proc := new(*harness.Proc)
	return harness.ProcSpec{
		Command:    buildArgs(r.cfg, spec),
		Dir:        spec.Workdir,
		Env:        mergeEnv(r.cfg.Env, spec.Env),
		InheritEnv: r.cfg.InheritEnv,
		RunID:      spec.RunID,
		Shim:       r.cfg.Shim,
		Stdin:      stdinText(spec),
		ParseLine:  ps.parseLine,
		OnExit: func(exitCode int, _ error) (harness.Result, error) {
			var tail []string
			if *proc != nil {
				tail = (*proc).StderrTail()
			}
			res := ps.result(exitCode, tail)
			res.Billing = r.billing(spec)
			return res, nil
		},
	}, proc
}

// stdinText is the prompt as codex receives it: instructions first, when there are any.
func stdinText(spec harness.Spec) string {
	if spec.Instructions == "" {
		return spec.Prompt
	}
	return spec.Instructions + instructionSeparator + spec.Prompt
}

// buildArgs returns the command line. A new session gets -C and the --approve-for-me default. A
// resumed one gets neither, because `codex exec resume` has no -C, --sandbox or --approve-for-me
// flags; instead it gets a config override that keeps the workspace-write sandbox. Either default
// is skipped when the config or the spec already chooses a sandbox.
func buildArgs(cfg harness.Config, spec harness.Spec) []string {
	args := []string{"codex"}
	if len(cfg.Command) > 0 {
		args = append([]string(nil), cfg.Command...)
	}
	args = append(args, "exec")
	resume := spec.ResumeSessionID != ""
	if resume {
		args = append(args, "resume", spec.ResumeSessionID)
	}
	args = append(args, "--json", "--skip-git-repo-check")
	if !resume && spec.Workdir != "" {
		args = append(args, "-C", spec.Workdir)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if spec.Effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+spec.Effort+`"`)
	}
	args = append(args, cfg.Args...)
	args = append(args, spec.Args...)
	if spec.PermissionMode != "" {
		// A mode chosen for the conversation replaces the default and wins over the profile's.
		args = append(args, permissionArgs(spec.PermissionMode, resume)...)
	} else if !hasSandboxFlag(cfg.Args, spec.Args) {
		if cfg.FullAccess {
			args = append(args, permissionArgs(permBypass, resume)...)
		} else if resume {
			args = append(args, "-c", resumeSandboxOverride)
		} else {
			args = append(args, defaultSandboxArgs...)
		}
	}
	// "-" reads the prompt from stdin.
	return append(args, "-")
}

// permissionArgs turns a permission mode choice into flags. `codex exec resume` has no sandbox or
// approval flags, so a resumed session gets config overrides with the same effect.
func permissionArgs(mode string, resume bool) []string {
	switch mode {
	case permApproveForMe:
		if resume {
			return []string{"-c", resumeSandboxOverride}
		}
		return defaultSandboxArgs
	case permBypass:
		if resume {
			return []string{"-c", `sandbox_mode="danger-full-access"`, "-c", `approval_policy="never"`}
		}
		return []string{"--dangerously-bypass-approvals-and-sandbox"}
	}
	if resume {
		return []string{"-c", `sandbox_mode="` + mode + `"`}
	}
	return []string{"--sandbox", mode}
}

// hasSandboxFlag reports whether an extra argument already chooses a sandbox or approval mode,
// either with a flag or with a config override of sandbox_mode (-c sandbox_mode=..., or the
// single-argument forms -c=... and --config=...).
func hasSandboxFlag(lists ...[]string) bool {
	const key = "sandbox_mode="
	for _, list := range lists {
		for i, a := range list {
			switch a {
			case "--sandbox", "-s", "--approve-for-me", "--full-auto",
				"--dangerously-bypass-approvals-and-sandbox":
				return true
			case "-c", "--config":
				if i+1 < len(list) && strings.HasPrefix(strings.TrimSpace(list[i+1]), key) {
					return true
				}
			}
			for _, p := range []string{"-c=", "--config="} {
				if v, ok := strings.CutPrefix(a, p); ok && strings.HasPrefix(strings.TrimSpace(v), key) {
					return true
				}
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
