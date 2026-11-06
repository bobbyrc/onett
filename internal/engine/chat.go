package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/store"
)

// chatTurnResult is what one orchestrator turn produced.
type chatTurnResult struct {
	Reply     string
	RunID     string // "" when no run started
	SessionID string // the harness session to resume next turn, "" if unknown
	Status    domain.RunStatus
	// Detached: onett is shutting down and the agent goes on running behind its shim. The turn is
	// not over; the next start reattaches to it (reattach.go), so nothing is stored or released.
	Detached bool
}

// chatOpts says what kind of turn chatReply runs.
type chatOpts struct {
	// goal marks a goal check-in turn: the run records it so a restart can still evaluate the
	// goal's progress when the turn ends (see finishGoalTurn).
	goal bool
}

// chatInterruptedNote is the prompt of a turn re-run because onett restarted mid-turn and the agent
// behind it was gone. The session resumes, so the agent has its own history.
const chatInterruptedNote = "onett was restarted mid-turn; your previous turn was interrupted. " +
	"Re-check state with the onett CLI before acting, since some commands may already have run."

// chatReply runs one orchestrator turn and stores its reply. The caller has already marked the
// conversation busy; this always clears the flag, even when the turn could not start. text is
// the turn's prompt and claimed the owner messages riding in it. When the turn succeeded and
// messages were queued meanwhile, the next turn starts with them; after any other outcome they
// stay queued until the owner sends or flushes (a denial or a stall is terminal).
// It reports whether the turn ended; false means it was detached at shutdown and is still running.
func (e *Engine) chatReply(ctx context.Context, conv domain.Conversation, text string, claimed []string, opts chatOpts) bool {
	res := e.chatTurn(ctx, conv, text, claimed, opts)
	return e.finishChat(ctx, conv, res, claimed)
}

// finishChat stores a turn's reply and releases the conversation. It is the end of every turn,
// whether it ran in this process or was reattached after a restart.
func (e *Engine) finishChat(ctx context.Context, conv domain.Conversation, res chatTurnResult, claimed []string) bool {
	if res.Detached {
		return false
	}
	reply := res.Reply
	if reply == "" {
		reply = "_(no reply)_"
	}
	// The turn may have timed out; the reply and the busy flag are still written.
	fin := context.WithoutCancel(ctx)
	msg, err := e.d.Store.AppendChatMessage(fin, domain.ChatMessage{
		ConversationID: conv.ID, Role: "assistant", Text: reply, RunID: res.RunID,
	})
	if err != nil {
		e.d.Logger.Error("chat: save reply", "conversation", conv.ID, "err", err)
	} else {
		e.d.Bus.Emit(fin, domain.EvChatMessage, conv.Project, "", res.RunID, "orchestrator replied", msg)
	}
	if res.RunID == "" {
		// The turn never got a run, so the messages in it were not sent. They wait in the queue
		// for the owner's next send or flush.
		e.restoreQueued(fin, claimed)
	}
	if _, err := e.d.Store.UpdateConversation(fin, conv.ID, func(c *domain.Conversation) error {
		if res.SessionID != "" {
			c.SessionID = res.SessionID
		}
		c.Busy = false
		c.ActiveRunID = ""
		return nil
	}); err != nil {
		e.d.Logger.Error("chat: release conversation", "conversation", conv.ID, "err", err)
		return true
	}
	// A message queued while this turn ended may have been stored after the queue was last
	// looked at; the busy flag is clear now, so this check cannot miss it.
	if res.Status == domain.RunSucceeded && ctx.Err() == nil {
		if err := e.FlushChat(fin, conv.ID); err != nil && !errors.Is(err, errStopped) {
			e.d.Logger.Error("chat: send queued messages", "conversation", conv.ID, "err", err)
		}
	}
	return true
}

// chatAgentName is the agent that chat runs on. It is a name in the agents config; the message
// author, ONETT_ROLE and closedBy "orchestrator" are separate identifiers and stay as they are.
const chatAgentName = "chief-of-staff"

// orchestratorAllowedTools pre-approves the onett CLI, which is how the orchestrator manages the
// factory, so Claude Code's auto-mode classifier does not refuse routine calls such as
// `onett task cancel`. Nothing else is allowed: no wildcard Bash.
var orchestratorAllowedTools = []string{"Bash(onett:*)"}

// chatTurn runs the orchestrator harness for one prompt. claimed are the messages in it; once
// the run exists they are stamped with its ID.
func (e *Engine) chatTurn(ctx context.Context, conv domain.Conversation, text string, claimed []string, opts chatOpts) chatTurnResult {
	fail := func(err error) chatTurnResult {
		return chatTurnResult{Reply: "I can't run: " + err.Error()}
	}
	cfg := e.Config()
	h, agent, err := e.harnessFor(conv.Project, chatAgentName)
	if err != nil {
		return fail(err)
	}
	if hold, ok := e.holds.held(agent.Harness, e.d.Now()); ok {
		return fail(errors.New(holdSummary(hold, e.d.Now()) + ". Send your message again after that"))
	}
	dir := filepath.Join(cfg.Server.DataDir, "chat", conv.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}
	base, err := e.AgentPrompt(agent)
	if err != nil {
		return fail(err)
	}

	// Settings the owner changed while an earlier turn ran take effect now; conv may predate that.
	settings := conv.Settings
	if cur, err := e.d.Store.GetConversation(ctx, conv.ID); err == nil {
		settings = cur.Settings
	}
	settings = effectiveSettings(agent, settings)

	now := e.d.Now().UTC()
	deadline := now.Add(cfg.Limits.RunTimeout.Duration)
	// The run row is bookkeeping for a reply that is already underway, so it is written even when
	// shutdown has cancelled ctx; otherwise the reply would fail to start and leave no run.
	run, err := e.d.Store.CreateRun(context.WithoutCancel(ctx), domain.Run{
		Project: conv.Project, Role: chatAgentName, Harness: agent.Harness, Model: settings.Model,
		Effort: settings.Effort, PermissionMode: settings.PermissionMode,
		Status: domain.RunStarting, Workdir: dir, GoalTurn: opts.goal,
		StartedAt: now, LastOutputAt: now, DeadlineAt: &deadline,
	})
	if err != nil {
		return fail(err)
	}
	e.d.Bus.Emit(ctx, domain.EvRunUpdated, conv.Project, "", run.ID, "orchestrator chat run starting", run)
	// From here the turn is live: later owner messages go to the agent or the queue, and the
	// messages in this prompt name the run.
	e.markTurnStarted(context.WithoutCancel(ctx), conv, run.ID, claimed)

	// Like a task run, the turn's context does not end with ctx: at shutdown cancelAllRuns ends
	// it, so that supervise can tell a shutdown (let the agent go on) from a cancel (kill it).
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Limits.RunTimeout.Duration)
	defer cancel()
	ar := e.registerRun(run.ID, "", conv.Project, cancel)
	defer e.unregisterRun(run.ID)
	if errors.Is(ctx.Err(), context.Canceled) {
		// Shutdown cancelled the reply before its run was registered, so cancelRun missed it.
		ar.reason.CompareAndSwap(nil, errStopped.Error())
		cancel()
	}

	// A harness that can reattach is started without the run's cancellation: supervise ends the
	// session when the context is done, and at shutdown it detaches instead.
	startCtx := runCtx
	if _, ok := h.(harness.Reattacher); ok {
		startCtx = context.WithoutCancel(runCtx)
	}
	prompt := text + factorySnapshot(cfg)
	sess, err := h.Start(startCtx, harness.Spec{
		RunID:           run.ID,
		Workdir:         dir,
		Prompt:          prompt,
		Instructions:    chatInstructions(conv, base),
		Model:           settings.Model,
		Effort:          settings.Effort,
		PermissionMode:  settings.PermissionMode,
		ResumeSessionID: conv.SessionID,
		Env:             e.chatEnv(conv, run.ID),
		Args:            agent.Args,
		AllowedTools:    append(slices.Clone(orchestratorAllowedTools), agent.Tools...),
		// Turns of one conversation share a long-lived agent process, so the prompt cache and
		// MCP connections carry over; a harness that cannot do that starts one per turn.
		ConversationID: conv.ID,
		KeepAlive:      max(cfg.Chat.SessionIdleTimeout.Duration, 0),
	})
	if err != nil {
		e.failChatRun(ctx, run, err)
		return chatTurnResult{Reply: "I couldn't start: " + err.Error(), RunID: run.ID, Status: domain.RunFailed}
	}
	ar.setSession(sess)
	e.appendRunLog(ctx, run.ID, domain.LogPrompt, prompt)

	return e.superviseChat(runCtx, conv, agent, run, sess, ar)
}

// chatEnv is the environment of an orchestrator turn. It alone gets the onett API token: it runs
// the onett CLI.
func (e *Engine) chatEnv(conv domain.Conversation, runID string) map[string]string {
	env := make(map[string]string, len(e.d.AgentEnv)+len(e.d.OrchestratorEnv)+4)
	for k, v := range e.d.AgentEnv {
		env[k] = v
	}
	for k, v := range e.d.OrchestratorEnv {
		env[k] = v
	}
	env["ONETT_CONVERSATION_ID"] = conv.ID
	env["ONETT_ROLE"] = "orchestrator"
	env["ONETT_RUN_ID"] = runID
	if conv.GoalID != "" {
		// Tasks the orchestrator creates in a goal conversation are linked to the goal.
		env["ONETT_GOAL"] = conv.GoalID
	}
	return env
}

// superviseChat follows a turn's session to its end and turns the outcome into the reply to
// store. It is the second half of chatTurn and what a reattached turn runs (resumeChat).
func (e *Engine) superviseChat(ctx context.Context, conv domain.Conversation, agent config.Agent, run domain.Run, sess harness.Session, ar *activeRun) chatTurnResult {
	res, status, reason := e.supervise(ctx, run, sess, ar, func(ev harness.Event) {
		if ev.Kind != domain.LogText || ev.Text == "" {
			return
		}
		// The map holds only strings, so it cannot fail to marshal.
		data, _ := json.Marshal(map[string]string{
			"conversation_id": conv.ID, "run_id": run.ID, "text": ev.Text,
		})
		if _, err := e.d.Bus.Publish(ctx, domain.Event{
			Type: domain.EvChatDelta, Project: conv.Project, RunID: run.ID, Data: data,
		}); err != nil {
			e.d.Logger.Warn("chat: publish delta", "err", err)
		}
	})

	if status == domain.RunRunning {
		return chatTurnResult{Detached: true}
	}
	reply := res.FinalText
	if res.RateLimit != nil && status == domain.RunCancelled {
		// A chat turn cannot wait for the limit to lift, so the owner is told and sends again.
		h := e.noteRateLimit(ctx, agent.Harness, *res.RateLimit)
		return chatTurnResult{Reply: "_(The " + agent.Harness + " harness hit its rate limit: " + holdSummary(h, e.d.Now()) +
			". Send your message again after that.)_", RunID: run.ID, SessionID: res.SessionID, Status: status}
	}
	if status != domain.RunSucceeded {
		note := "_(The orchestrator run ended: " + string(status)
		if reason != "" {
			note += ": " + reason
		}
		note += ".)_"
		if reply != "" {
			reply += "\n\n"
		}
		reply += note
	}
	return chatTurnResult{Reply: reply, RunID: run.ID, SessionID: res.SessionID, Status: status}
}

// markTurnStarted records the turn's run on the conversation and on the messages it carries.
func (e *Engine) markTurnStarted(ctx context.Context, conv domain.Conversation, runID string, claimed []string) {
	if _, err := e.d.Store.UpdateConversation(ctx, conv.ID, func(c *domain.Conversation) error {
		c.ActiveRunID = runID
		return nil
	}); err != nil {
		e.d.Logger.Error("chat: record active run", "conversation", conv.ID, "err", err)
	}
	for _, id := range claimed {
		m, err := e.d.Store.UpdateChatMessage(ctx, id, func(m *domain.ChatMessage) error {
			m.Queued = false
			m.RunID = runID
			return nil
		})
		if err != nil {
			// A message that is gone has nothing to stamp.
			if !errors.Is(err, store.ErrNotFound) {
				e.d.Logger.Error("chat: stamp message", "message", id, "err", err)
			}
			continue
		}
		e.d.Bus.Emit(ctx, domain.EvChatMessage, conv.Project, "", runID, "message reached the orchestrator", m)
	}
}

// failChatRun marks a chat run that never started as failed, so it does not stay "starting".
func (e *Engine) failChatRun(ctx context.Context, run domain.Run, cause error) {
	now := e.d.Now().UTC()
	r, err := e.d.Store.UpdateRun(ctx, run.ID, func(r *domain.Run) error {
		r.Status, r.Error, r.EndedAt = domain.RunFailed, cause.Error(), &now
		return nil
	})
	if err != nil {
		e.d.Logger.Error("chat: fail run", "run", run.ID, "err", err)
		return
	}
	e.d.Bus.Emit(ctx, domain.EvRunUpdated, r.Project, "", r.ID, "orchestrator chat run failed to start", r)
}

// chatInstructions is the orchestrator's system prompt: its role prompt plus what is fixed for
// the conversation. It must be byte-identical on every turn: the system prompt is the front of
// the prompt cache, so any change makes the whole history miss the cache. Anything that can
// change between turns (the projects and agents that exist) goes in factorySnapshot, which rides
// in the user message instead.
func chatInstructions(conv domain.Conversation, base string) string {
	var b strings.Builder
	b.WriteString(base)
	if conv.Project != "" {
		fmt.Fprintf(&b, "\n\nDefault project for this conversation: %s.", conv.Project)
	}
	if conv.GoalID != "" {
		b.WriteString(goalInstructions(conv.GoalID))
	}
	return b.String()
}

// factorySnapshot lists the projects and agents that exist right now. It is appended to each
// turn's prompt rather than the system prompt (see chatInstructions).
func factorySnapshot(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("\n\n---\n## The factory right now\n\n")
	for _, name := range cfg.ProjectNames() {
		p := cfg.Projects[name]
		paused := "no"
		if p.Paused {
			paused = "yes"
		}
		fmt.Fprintf(&b, "- **%s** — %s, %s, paused: %s\n", name, p.Repo, p.Path, paused)
	}

	// A disabled agent is left out: the orchestrator must not hand it work.
	agents := make([]string, 0, len(cfg.Agents))
	for name, a := range cfg.Agents {
		if !a.Disabled {
			agents = append(agents, name)
		}
	}
	sort.Strings(agents)
	b.WriteString("\nAgents:\n")
	for _, name := range agents {
		a := cfg.Agents[name]
		who := a.Harness
		if a.Model != "" {
			who += "/" + a.Model
		}
		fmt.Fprintf(&b, "- **%s** (%s) — %s; handles: %s\n", name, who, a.Description, handledKinds(cfg, name))
	}
	return b.String()
}

// handledKinds lists the task kinds routed to an agent, or "none".
func handledKinds(cfg *config.Config, agent string) string {
	var kinds []string
	for kind, name := range cfg.Routing {
		if name == agent {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return "none"
	}
	sort.Strings(kinds)
	return strings.Join(kinds, ", ")
}
