package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/sysproc"
	"github.com/bobbyrc/onett/internal/workspace"
)

// Reattaching to a run after onett restarted (limits.run_shim).
//
// With the shim on, an agent outlives the daemon: at shutdown supervise lets go of it (see
// cancelAllRuns) and leaves the run and its task running. On the next start, recover calls
// reattachRun for each run still marked running. If the run's shim is alive it dials it, replays
// the agent's output from the stored offset and supervises the run again, with the stall clock and
// the run timeout carried over from before the restart. Finishing the run is then the same code
// as for a run that never restarted (settleTask).
//
// Covered: task runs, including review runs. Not covered: orchestrator chat turns and goal turns
// (chat.go, loop_goals.go). They are killed at shutdown as before, and recover fails them. A
// reattached task run also does not know about three things only the first process knew: whether
// setup was still owed after the agent (taskRun.setupOwed), the owner messages the run claimed
// (a failed follow-up is retried like a plain run), and its queue-continuation root when the
// conversation had no claimed messages. A run whose shim is gone is failed and its task re-queued,
// as before.

// reattachRun tries to take a running run over again after a restart. It reports whether it did:
// true means supervision continues in a goroutine and recover must leave the run and its task
// alone; false means recover handles the run as it always has.
func (e *Engine) reattachRun(ctx context.Context, r domain.Run) bool {
	if r.Status != domain.RunRunning || r.TaskID == "" || r.Workdir == "" || r.StartSHA == "" {
		return false
	}
	log := e.d.Logger.With("run", r.ID, "task", r.TaskID)
	cfg := e.Config()
	runDir := runshim.RunDir(cfg.Server.DataDir, r.ID)
	st, err := runshim.ReadStatus(runDir)
	if err != nil || !pidAlive(st.ShimPID) {
		return false // no shim, or it has exited: nothing to reattach to
	}
	if e.takeOver(ctx, r, log, cfg) {
		return true
	}
	// The shim is alive but the run cannot be supervised (its task moved on, the project or
	// harness is gone, the workdir is missing...). recover fails the run next; without this the
	// shim would keep an unwatched agent alive, waiting for an ack that never comes.
	e.terminateShim(log, st)
	return false
}

// takeOver is the part of reattachRun that needs a live shim; false means the run was not taken.
func (e *Engine) takeOver(ctx context.Context, r domain.Run, log *slog.Logger, cfg *config.Config) bool {
	t, err := e.d.Store.GetTask(ctx, r.TaskID)
	if err != nil || t.Status != domain.TaskRunning {
		return false
	}
	p, ok := cfg.Projects[t.Project]
	if !ok {
		return false
	}
	h, agent, err := e.harnessFor(t.Project, t.Role)
	if err != nil {
		return false
	}
	ra, ok := h.(harness.Reattacher)
	if !ok {
		return false
	}
	var parent *domain.Task
	if t.Kind == domain.KindReview {
		pt, err := e.d.Store.GetTask(ctx, t.ParentID)
		if err != nil {
			return false
		}
		parent = &pt
	}
	if fi, err := os.Stat(r.Workdir); err != nil || !fi.IsDir() {
		return false
	}
	ws := workspace.Workspace{
		Dir: r.Workdir, Branch: r.Branch, BaseRef: "origin/" + p.DefaultBranch,
		StartSHA: r.StartSHA, RemoteSHA: r.RemoteSHA,
	}
	env := e.taskEnv(t, r, p, ws)
	// The harness rebuilds its output parser from the spec; the prompt and instructions only
	// matter to a process being started, so they are not needed here.
	spec := harness.Spec{
		RunID: r.ID, Workdir: ws.Dir, Model: agent.Model, Env: env, Args: agent.Args,
	}

	// The run keeps its original deadline, measured on the engine's clock.
	timeout := cfg.Limits.RunTimeout.Duration
	if r.DeadlineAt != nil {
		timeout = r.DeadlineAt.Sub(e.d.Now())
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), max(timeout, 0)) // as in startTask
	// Like runTask: supervise ends the session on runCtx, so the harness must not do it too.
	sess, err := ra.Reattach(context.WithoutCancel(ctx), spec, r.StreamOffset)
	if err != nil {
		cancel()
		log.Warn("could not reattach to the run's shim; failing the run", "err", err)
		return false
	}
	ar := e.registerRun(r.ID, t.ID, t.Project, cancel)
	ar.reattached = true
	ar.setSession(sess)

	root := t
	if rt, err := e.conversationRoot(ctx, t); err == nil {
		root = rt
	}
	taskState := &taskRun{
		t: t, run: r, ar: ar, p: p, agent: agent, parent: parent, repo: e.repoFor(p), ws: ws,
		cmdEnv: sysproc.AgentEnv(cfg.Harness[agent.Harness].InheritEnv, env),
		root:   root, fail: e.requeueOrBlock,
	}
	e.appendRunLog(context.WithoutCancel(ctx), r.ID, domain.LogSystem,
		fmt.Sprintf("onett restarted; reattached to the agent at stream offset %d", r.StreamOffset))
	started := e.goTracked(func() {
		func() {
			defer e.unregisterRun(r.ID)
			defer cancel()
			e.resumeTask(runCtx, taskState, sess)
		}()
		if ar.continueRoot != "" {
			e.continueConversation(context.WithoutCancel(ctx), ar.continueRoot)
		}
	})
	if !started {
		// Shutting down again already: let go of the agent once more.
		cancel()
		if d, ok := sess.(harness.Detacher); ok {
			d.Detach()
		}
		e.unregisterRun(r.ID)
	}
	return true
}

// resumeTask supervises a reattached run and finishes it, like the second half of runTask.
func (e *Engine) resumeTask(ctx context.Context, st *taskRun, sess harness.Session) {
	bg := context.WithoutCancel(ctx)
	defer e.sweepWorktrees(bg, st.p)
	defer e.guard(bg, st)
	res, status, reason := e.supervise(ctx, st.run, sess, st.ar, nil)
	if status == domain.RunRunning {
		st.cancelled = true // shutting down again; the next start reattaches again
		return
	}
	e.settleTask(ctx, st, res, status, reason)
}

// shimKillGrace is how long terminateShim waits after SIGTERM before SIGKILL (as the harness's
// procKillGrace), and shimExitWait how long it waits for the shim to wind down after the ack.
const (
	shimKillGrace = 10 * time.Second
	shimExitWait  = 5 * time.Second
)

// terminateShim stops the agent behind a shim nobody will supervise: SIGTERM to its process group,
// SIGKILL after shimKillGrace, then it acks the final offset so the shim exits. Best effort: if
// the shim cannot be dialled it is left to its own grace period.
func (e *Engine) terminateShim(log *slog.Logger, st runshim.Status) {
	c, err := runshim.Dial(st.Socket)
	if err != nil {
		log.Warn("could not reach the shim to stop its agent", "err", err)
		return
	}
	defer c.Close()
	exited := func() (runshim.Status, bool) {
		s, err := c.Status()
		return s, err != nil || s.Exited
	}
	waitExit := func(d time.Duration) (runshim.Status, bool) {
		deadline := time.Now().Add(d)
		for {
			if s, ok := exited(); ok {
				return s, true
			}
			if time.Now().After(deadline) {
				return runshim.Status{}, false
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	_ = c.Signal(syscall.SIGTERM)
	s, ok := waitExit(shimKillGrace)
	if !ok {
		_ = c.Signal(syscall.SIGKILL)
		s, _ = waitExit(shimKillGrace)
	}
	if s.Exited {
		_ = c.Ack(s.LogSize)
	}
	// Give the shim a moment to remove its socket and exit.
	rtDeadline := time.Now().Add(shimExitWait)
	for pidAlive(st.ShimPID) && time.Now().Before(rtDeadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

// pidAlive reports whether a process with this pid exists. A pid owned by another user still
// counts: the shim is ours, so that would be a recycled pid, which the dial then rejects.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
