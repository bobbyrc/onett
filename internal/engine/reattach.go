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
	"github.com/bobbyrc/onett/internal/store"
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
// Covered: task runs, including review runs, and orchestrator chat turns and goal turns (resumeChat,
// which finishes the turn as chatReply does). Setup owed after the agent (taskRun.setupOwed) and the
// owner messages a task run claimed are restored from the run row. A task run whose shim is gone ends
// interrupted and resumes from its session. A chat turn whose shim is gone is not failed: recover
// re-runs it by resuming the conversation's session with chatInterruptedNote (rerunInterrupted); an
// interrupted goal turn is exempt from max_goal_turns, and its re-run is judged like any goal turn.

// reattachRun tries to take a running run over again after a restart. It reports whether it did:
// true means supervision continues in a goroutine and recover must leave the run and its task
// alone; false means recover handles the run as it always has.
func (e *Engine) reattachRun(ctx context.Context, r domain.Run) bool {
	chat := r.TaskID == ""
	if chat {
		if config.CanonicalAgentName(r.Role) != chatAgentName || r.Workdir == "" {
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
		// The run's own log says this too, but the daemon log is where an operator looks after a
		// restart, so each reattached run gets one line there.
		log.Info("reattached to a run after restart", "shim_pid", st.ShimPID, "stream_offset", r.StreamOffset)
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
			root:   root, fail: e.requeueOrBlock, setupOwed: r.SetupOwed,
		}
		if claimed := e.claimedBy(ctx, root, r.ID); len(claimed) > 0 {
			taskState.fail = e.failFollowUp(claimed)
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
	h, agent, err := e.harnessFor(conv.Project, chatAgentName)
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
// the conversation's session with chatInterruptedNote. The conversation stays busy for it.
// endedByRestart holds the runs recover ended on any start; a busy conversation whose run ended on
// its own (a crash between storing the reply and clearing the flag) finished its turn, so it is
// only released. An interrupted goal turn is exempt from max_goal_turns: the re-run takes its
// place in the goal's count rather than adding one, and is then judged by the goal guard like any
// goal turn. It reports whether a turn was started; false leaves the conversation for recover
// to release.
func (e *Engine) rerunInterrupted(ctx context.Context, c domain.Conversation, endedByRestart map[string]bool) bool {
	if c.TaskID != "" || c.ClosedAt != nil || c.ActiveRunID == "" || !endedByRestart[c.ActiveRunID] {
		return false
	}
	run, err := e.d.Store.GetRun(ctx, c.ActiveRunID)
	if err != nil {
		return false
	}
	var before map[string]time.Time
	if run.GoalTurn {
		g, err := e.d.Store.GetGoal(ctx, c.GoalID)
		if err != nil || g.Status != domain.GoalActive {
			return false // the goal moved on; nothing to resume
		}
		// The interrupted turn was counted when it started; the re-run stands in for it.
		tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{GoalID: g.ID, Limit: 500})
		if err != nil {
			return false
		}
		before = make(map[string]time.Time, len(tasks))
		for _, t := range tasks {
			before[t.ID] = t.UpdatedAt
		}
	}
	timeout := e.Config().Limits.RunTimeout.Duration
	base := e.replyBase()
	return e.goTracked(func() {
		runCtx, cancel := context.WithTimeout(base, timeout)
		defer cancel()
		if e.chatReply(runCtx, c, chatInterruptedNote, nil, chatOpts{goal: run.GoalTurn}) && run.GoalTurn {
			e.goalGuard(context.WithoutCancel(runCtx), c.GoalID, before)
		}
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

// claimedBy returns the owner messages that the run took from root's conversation.
func (e *Engine) claimedBy(ctx context.Context, root domain.Task, runID string) []domain.ChatMessage {
	conv, err := e.d.Store.GetConversationByTask(ctx, root.ID)
	if err != nil {
		return nil
	}
	msgs, err := e.d.Store.ListChatMessages(ctx, conv.ID)
	if err != nil {
		return nil
	}
	var out []domain.ChatMessage
	for _, m := range msgs {
		if m.RunID == runID {
			out = append(out, m)
		}
	}
	return out
}

// interruptible reports whether recover should mark r interrupted rather than failed: it is a run
// of a task that is still running, so the task can pick it up again.
func (e *Engine) interruptible(ctx context.Context, r domain.Run) bool {
	if r.TaskID == "" {
		return false // chat and goal turns are failed, as before
	}
	t, err := e.d.Store.GetTask(ctx, r.TaskID)
	return err == nil && t.Status == domain.TaskRunning
}

// interruptedResume looks for an interrupted run before the current one. It returns the session
// to resume ("" when that run never reported one, or ran on another harness) and whether there was
// such a run at all.
func (e *Engine) interruptedResume(ctx context.Context, t domain.Task, current domain.Run, harnessName string) (string, bool) {
	runs, err := e.d.Store.ListRuns(ctx, store.RunFilter{TaskID: t.ID, Limit: 2})
	if err != nil {
		return "", false
	}
	for _, r := range runs {
		if r.ID == current.ID {
			continue
		}
		if r.Status != domain.RunInterrupted {
			return "", false
		}
		if r.Harness != harnessName {
			return "", true
		}
		return r.SessionID, true
	}
	return "", false
}

// ingestStreamLog stores what the agent of an interrupted run wrote after the last line onett
// stored, which is still in the dead shim's stream.log. Without it that output, and any session ID
// it carried, would be lost to the restart. Best effort: a run that cannot be replayed is
// interrupted all the same.
func (e *Engine) ingestStreamLog(ctx context.Context, r domain.Run) {
	log := e.d.Logger.With("run", r.ID, "task", r.TaskID)
	t, err := e.d.Store.GetTask(ctx, r.TaskID)
	if err != nil {
		log.Warn("not ingesting the interrupted run's stream log: task lookup failed", "err", err)
		return
	}
	h, agent, err := e.harnessFor(t.Project, t.Role)
	if err != nil {
		log.Warn("not ingesting the interrupted run's stream log: no harness", "err", err)
		return
	}
	lr, ok := h.(harness.LogReplayer)
	if !ok {
		return // this harness cannot replay a log; not worth a warning
	}
	evs, err := lr.ReplayLog(harness.Spec{
		RunID: r.ID, Workdir: r.Workdir, Model: agent.Model, Args: agent.Args,
	}, r.StreamOffset)
	if err != nil {
		log.Warn("could not read the interrupted run's stream log", "err", err)
		return
	}
	if len(evs) == 0 {
		return
	}
	var lines []domain.LogLine
	var offset int64
	var sessionID string
	for _, ev := range evs {
		offset = max(offset, ev.Offset)
		if sessionID == "" {
			sessionID = ev.SessionID
		}
		at := ev.Time
		if at.IsZero() {
			at = e.d.Now()
		}
		lines = append(lines, domain.LogLine{RunID: r.ID, Time: at, Kind: ev.Kind, Text: ev.Text, ToolID: ev.ToolID})
	}
	// Lines first, then the offset past them, as supervise does: a crash between the two repeats
	// lines rather than losing them.
	if err := e.storeRunLogs(ctx, r, lines); err != nil {
		return // offset stays put, so nothing is skipped
	}
	e.updateRun(ctx, r.ID, func(r *domain.Run) error {
		r.StreamOffset = max(r.StreamOffset, offset)
		if r.SessionID == "" {
			r.SessionID = sessionID
		}
		return nil
	})
	e.appendRunLog(ctx, r.ID, domain.LogSystem,
		fmt.Sprintf("recovered %d lines of output the agent wrote before onett stopped", len(lines)))
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
	c, err := runshim.DialStatus(st)
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
