package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
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
// Covered: task runs, including review runs, and orchestrator chat turns and goal turns (reattachChat,
// which finishes the turn as chatReply does). A chat turn whose shim is gone is not failed: recover
// re-runs it by resuming the conversation's session with interruptedNote (rerunInterrupted); an
// interrupted goal turn is not counted and not judged. A reattached task run also does not know about three things only the first process knew: whether
// setup was still owed after the agent (taskRun.setupOwed), the owner messages the run claimed
// (a failed follow-up is retried like a plain run), and its queue-continuation root when the
// conversation had no claimed messages. A run whose shim is gone is failed and its task re-queued,
// as before.

// reattachRun tries to take a running run over again after a restart. It reports whether it did:
// true means supervision continues in a goroutine and recover must leave the run and its task
// alone; false means recover handles the run as it always has.
func (e *Engine) reattachRun(ctx context.Context, r domain.Run) bool {
	chat := r.TaskID == ""
	if chat {
		if r.Role != "orchestrator" || r.Workdir == "" {
			return false
		}
	} else if r.Status != domain.RunRunning || r.Workdir == "" || r.StartSHA == "" {
		return false
	}
	log := e.d.Logger.With("run", r.ID, "task", r.TaskID)
	cfg := e.Config()
	runDir := runshim.RunDir(cfg.Server.DataDir, r.ID)
	st, err := runshim.ReadStatus(runDir)
	if err != nil || !pidAlive(st.ShimPID) {
		return false // no shim, or it has exited: nothing to reattach to
	}
	took := false
	if chat {
		took = e.takeOverChat(ctx, r, log, cfg)
	} else {
		took = e.takeOver(ctx, r, log, cfg)
	}
	if took {
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

	root := t
	if rt, err := e.conversationRoot(ctx, t); err == nil {
		root = rt
	}
	return e.startReattached(ctx, r, log, cfg, ra, spec, func(runCtx context.Context, ar *activeRun, sess harness.Session) {
		taskState := &taskRun{
			t: t, run: r, ar: ar, p: p, agent: agent, parent: parent, repo: e.repoFor(p), ws: ws,
			cmdEnv: sysproc.AgentEnv(cfg.Harness[agent.Harness].InheritEnv, env),
			root:   root, fail: e.requeueOrBlock,
		}
		e.resumeTask(runCtx, taskState, sess)
	}, func(ar *activeRun) {
		if ar.continueRoot != "" {
			e.continueConversation(context.WithoutCancel(ctx), ar.continueRoot)
		}
	})
}

// takeOverChat is takeOver for an orchestrator turn: the conversation whose active run this is.
func (e *Engine) takeOverChat(ctx context.Context, r domain.Run, log *slog.Logger, cfg *config.Config) bool {
	convs, err := e.d.Store.ListConversations(ctx, 1000)
	if err != nil {
		return false
	}
	i := slices.IndexFunc(convs, func(c domain.Conversation) bool {
		return c.ActiveRunID == r.ID && c.Busy && c.TaskID == ""
	})
	if i < 0 {
		return false
	}
	conv := convs[i]
	h, agent, err := e.harnessFor(conv.Project, "orchestrator")
	if err != nil {
		return false
	}
	ra, ok := h.(harness.Reattacher)
	if !ok {
		return false
	}
	spec := harness.Spec{
		RunID: r.ID, Workdir: r.Workdir, Model: agent.Model, Env: e.chatEnv(conv, r.ID), Args: agent.Args,
	}
	return e.startReattached(ctx, r, log, cfg, ra, spec, func(runCtx context.Context, ar *activeRun, sess harness.Session) {
		e.resumeChat(runCtx, conv, agent, r, sess, ar)
	}, nil)
}

// startReattached is the part of taking a run over that task runs and chat turns share: it
// dials the shim, registers the run (with the stall clock and deadline carried over) and
// supervises it in a goroutine through run. after (may be nil) runs once the run is
// unregistered. It reports whether the run was taken.
func (e *Engine) startReattached(ctx context.Context, r domain.Run, log *slog.Logger, cfg *config.Config,
	ra harness.Reattacher, spec harness.Spec,
	run func(runCtx context.Context, ar *activeRun, sess harness.Session), after func(ar *activeRun)) bool {
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
	ar := e.registerRun(r.ID, r.TaskID, r.Project, cancel)
	ar.reattached = true
	ar.setSession(sess)

	e.appendRunLog(context.WithoutCancel(ctx), r.ID, domain.LogSystem,
		fmt.Sprintf("onett restarted; reattached to the agent at stream offset %d", r.StreamOffset))
	started := e.goTracked(func() {
		func() {
			defer e.unregisterRun(r.ID)
			defer cancel()
			run(runCtx, ar, sess)
		}()
		if after != nil {
			after(ar)
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

// resumeChat supervises a reattached orchestrator turn and finishes it, like the second half of
// chatTurn followed by chatReply, and the goal's after-turn check when it was a goal turn.
func (e *Engine) resumeChat(ctx context.Context, conv domain.Conversation, agent config.Agent, run domain.Run, sess harness.Session, ar *activeRun) {
	res := e.superviseChat(ctx, conv, agent, run, sess, ar)
	if !e.finishChat(ctx, conv, res, nil) {
		return // shutting down again; the next start reattaches again
	}
	if run.GoalTurn && conv.GoalID != "" {
		e.finishGoalTurn(context.WithoutCancel(ctx), conv.GoalID)
	}
}

// rerunInterrupted is recover's answer to an orchestrator turn whose agent is gone (its shim did
// not survive, or run_shim was off): rather than lose the turn it starts a new one that resumes
// the conversation's session with interruptedNote. The conversation stays busy for it. A goal turn
// interrupted this way is taken back out of the goal's turn count, and the re-run is not judged
// by the goal guard, so a restart can neither use up the goal's turns nor block it. It reports
// whether a turn was started; false leaves the conversation for recover to release.
func (e *Engine) rerunInterrupted(ctx context.Context, c domain.Conversation) bool {
	if c.TaskID != "" || c.ClosedAt != nil || c.ActiveRunID == "" {
		return false
	}
	run, err := e.d.Store.GetRun(ctx, c.ActiveRunID)
	if err != nil {
		return false
	}
	if run.GoalTurn {
		g, err := e.d.Store.GetGoal(ctx, c.GoalID)
		if err != nil || g.Status != domain.GoalActive {
			return false // the goal moved on; nothing to resume
		}
		if _, err := e.d.Store.UpdateGoal(ctx, g.ID, func(g *domain.Goal) error {
			if g.Status != domain.GoalActive {
				return errSkip
			}
			g.Turns = max(g.Turns-1, 0)
			return nil
		}); err != nil && !errors.Is(err, errSkip) {
			e.d.Logger.Error("recover: uncount interrupted goal turn", "goal", g.ID, "err", err)
		}
	}
	timeout := e.Config().Limits.RunTimeout.Duration
	base := e.replyBase()
	return e.goTracked(func() {
		runCtx, cancel := context.WithTimeout(base, timeout)
		defer cancel()
		e.chatReply(runCtx, c, interruptedNote, nil, chatOpts{})
	})
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
