// Package domain holds the types shared by every onett package. It has no dependencies on
// other onett packages, and its JSON tags are the wire format of the HTTP API.
package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// Risk orders how dangerous a change is. Its zero value is RiskUnknown, which policy treats as
// high.
type Risk string

const (
	RiskUnknown Risk = ""
	RiskLow     Risk = "low"
	RiskMedium  Risk = "medium"
	RiskHigh    Risk = "high"
)

// Rank returns 0 for low, 1 for medium and 2 for high or unknown.
func (r Risk) Rank() int {
	switch r {
	case RiskLow:
		return 0
	case RiskMedium:
		return 1
	default:
		return 2
	}
}

// MaxRisk returns the more dangerous of a and b; unknown counts as high.
func MaxRisk(a, b Risk) Risk {
	return [...]Risk{RiskLow, RiskMedium, RiskHigh}[max(a.Rank(), b.Rank())]
}

type TaskKind string

const (
	KindFeature  TaskKind = "feature"
	KindBugfix   TaskKind = "bugfix"
	KindIssue    TaskKind = "issue"
	KindIncident TaskKind = "incident"
	KindReview   TaskKind = "review"
	KindChore    TaskKind = "chore"
	KindImprove  TaskKind = "improve"
	KindQuestion TaskKind = "question"
)

type TaskStatus string

const (
	TaskQueued           TaskStatus = "queued"
	TaskRunning          TaskStatus = "running"
	TaskReview           TaskStatus = "review"
	TaskAwaitingApproval TaskStatus = "awaiting_approval"
	TaskMerged           TaskStatus = "merged"
	TaskDeploying        TaskStatus = "deploying"
	TaskDone             TaskStatus = "done"
	TaskBlocked          TaskStatus = "blocked"
	TaskFailed           TaskStatus = "failed"
	TaskCancelled        TaskStatus = "cancelled"
)

// Terminal reports whether no loop will move the task on without a human.
func (s TaskStatus) Terminal() bool {
	switch s {
	case TaskDone, TaskBlocked, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// Task is one unit of work for one agent role in one project.
type Task struct {
	ID          string     `json:"id"`
	Project     string     `json:"project"`
	ParentID    string     `json:"parent_id,omitempty"`
	Kind        TaskKind   `json:"kind"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Role        string     `json:"role"`
	Status      TaskStatus `json:"status"`
	Priority    int        `json:"priority"` // higher runs first
	Risk        Risk       `json:"risk,omitempty"`
	RiskReasons []string   `json:"risk_reasons,omitempty"`
	// Source says where the task came from: "chat", "api", "github_issue", "signal",
	// "friction", "followup", "shepherd", "conversation".
	Source string `json:"source"`
	// SourceRef dedupes tasks per project: "github_issue:42", "signal:<fingerprint>".
	SourceRef string `json:"source_ref,omitempty"`
	Branch    string `json:"branch,omitempty"`
	PRNumber  int    `json:"pr_number,omitempty"`
	PRURL     string `json:"pr_url,omitempty"`
	MergeSHA  string `json:"merge_sha,omitempty"`
	// ApprovedSHA is the PR head commit a reviewer run approved; a new push invalidates it.
	ApprovedSHA string `json:"approved_sha,omitempty"`
	// PushedAt is when onett last pushed the task branch (CI may not have registered yet).
	PushedAt *time.Time `json:"pushed_at,omitempty"`
	// CIHeadSHA is the PR head the shepherd last saw, and CIHeadSeenAt when it first saw it;
	// the CI grace period runs from there, so any push (onett's or a human's) gets one.
	CIHeadSHA     string     `json:"ci_head_sha,omitempty"`
	CIHeadSeenAt  *time.Time `json:"ci_head_seen_at,omitempty"`
	Summary       string     `json:"summary,omitempty"` // latest agent summary
	BlockedReason string     `json:"blocked_reason,omitempty"`
	// FollowUpPending marks a task queued by an owner message to its agent: its next run resumes
	// the agent's session with the queued messages. An automatic retry never has it, so a retry
	// after a failure keeps its attempt notes and leaves queued messages alone.
	FollowUpPending bool      `json:"follow_up_pending,omitempty"`
	Attempts        int       `json:"attempts"`
	MaxAttempts     int       `json:"max_attempts"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// CreatedByRun is the run that created this task, when an agent created it.
	CreatedByRun string `json:"created_by_run,omitempty"`
	// GoalID is the goal this task works toward, when it was queued for one.
	GoalID string `json:"goal_id,omitempty"`
	// WaitingOn lists the tasks that must land (merge, or finish without a pull request) before
	// the scheduler may claim this queued task. Onett clears an entry when that task lands, and
	// cancels this task when one of them ends without landing.
	WaitingOn []string `json:"waiting_on,omitempty"`
}

type RunStatus string

const (
	RunStarting  RunStatus = "starting"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunStalled   RunStatus = "stalled"
	RunTimedOut  RunStatus = "timed_out"
	RunDenied    RunStatus = "denied"
	RunCancelled RunStatus = "cancelled"
	// RunInterrupted: onett restarted and the run's agent was gone with nothing to reattach to
	// (reboot, shim crash). It is not an agent failure: the task is queued again and its next run
	// resumes the agent's session without using an attempt.
	RunInterrupted RunStatus = "interrupted"
)

// Run is one harness session working on a task.
type Run struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	Project      string     `json:"project"`
	Role         string     `json:"role"`
	Harness      string     `json:"harness"`
	Model        string     `json:"model,omitempty"`
	Status       RunStatus  `json:"status"`
	SessionID    string     `json:"session_id,omitempty"` // harness session, for resume
	Workdir      string     `json:"workdir,omitempty"`
	Branch       string     `json:"branch,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	LastOutputAt time.Time  `json:"last_output_at"`
	ExitCode     int        `json:"exit_code"`
	CostUSD      float64    `json:"cost_usd"`
	// CostEstimated: CostUSD was computed from tokens and the configured price, not reported.
	CostEstimated bool `json:"cost_estimated,omitempty"`
	// Billing is "metered", "subscription" or "" (unknown). A subscription run's CostUSD is an
	// estimate nobody is billed for, so it does not count toward the daily budget.
	Billing      string `json:"billing,omitempty"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Summary      string `json:"summary,omitempty"`
	Error        string `json:"error,omitempty"`

	// The next five let a restarted onett pick the run up again (limits.run_shim). They live in
	// the run's JSON document, so they need no schema change.
	//
	// StreamOffset is where in the shim's stdout log to read next: every line before it has been
	// stored in the run's log. DeadlineAt is when the run times out (started + limits.run_timeout),
	// kept absolute so a restart does not grant a new full timeout. StartSHA and RemoteSHA are the
	// workspace's HEAD and origin/<branch> when the agent started; finishing the run needs both.
	StreamOffset int64      `json:"stream_offset,omitempty"`
	DeadlineAt   *time.Time `json:"deadline_at,omitempty"`
	StartSHA     string     `json:"start_sha,omitempty"`
	RemoteSHA    string     `json:"remote_sha,omitempty"`
	// GoalTurn marks an orchestrator turn that was a goal check-in, so a restarted onett can still
	// run the goal's after-turn check when the turn ends.
	GoalTurn bool `json:"goal_turn,omitempty"`
	// SetupOwed: project setup did not pass before the agent started, so it runs again after the
	// agent's turn. A reattached run needs it to finish like a run that never restarted.
	SetupOwed bool `json:"setup_owed,omitempty"`
}

// LogKind classifies one line of a run's streamed output.
type LogKind string

const (
	LogText       LogKind = "text"        // assistant prose
	LogToolUse    LogKind = "tool_use"    // the agent called a tool
	LogToolResult LogKind = "tool_result" // a tool returned
	LogDenial     LogKind = "denial"      // a tool call was refused
	LogSystem     LogKind = "system"      // onett or harness bookkeeping
	LogStderr     LogKind = "stderr"
	LogError      LogKind = "error"
	LogPrompt     LogKind = "prompt" // the text onett gave the agent to start a run or turn
	LogOwner      LogKind = "owner"  // a message from the owner, sent into a run
)

// LogLine is one entry in a run's log. Seq is assigned by the store, starting at 1 per run.
type LogLine struct {
	RunID string    `json:"run_id"`
	Seq   int64     `json:"seq"`
	Time  time.Time `json:"time"`
	Kind  LogKind   `json:"kind"`
	Text  string    `json:"text"`
	// ToolID pairs a tool_use line with its tool_result line; empty on other lines.
	ToolID string `json:"tool_id,omitempty"`
}

// Event is an append-only record of something that happened. ID is assigned by the store and
// increases monotonically, so clients resume an SSE stream with the last ID they saw.
type Event struct {
	ID      int64           `json:"id"`
	Time    time.Time       `json:"time"`
	Type    string          `json:"type"`
	Project string          `json:"project,omitempty"`
	TaskID  string          `json:"task_id,omitempty"`
	RunID   string          `json:"run_id,omitempty"`
	Message string          `json:"message"` // one human-readable line
	Data    json.RawMessage `json:"data,omitempty"`
}

// Event types. Clients must ignore types they do not know.
// Types marked "live" are fanned out to subscribers but not persisted.
const (
	EvTaskCreated         = "task.created" // Data: Task
	EvTaskUpdated         = "task.updated" // Data: Task
	EvRunUpdated          = "run.updated"  // Data: Run (start, status change, finish)
	EvRunLog              = "run.log"      // Data: []LogLine; live
	EvPROpened            = "pr.opened"
	EvPRMerged            = "pr.merged"
	EvPolicyDecision      = "policy.decision"    // Data: policy.Decision
	EvApprovalRequested   = "approval.requested" // Data: Approval
	EvApprovalDecided     = "approval.decided"   // Data: Approval
	EvDeployUpdated       = "deploy.updated"     // Data: Deployment
	EvSignalNew           = "signal.new"         // Data: Signal
	EvFrictionReported    = "friction.reported"  // Data: Friction
	EvIssueFiled          = "issue.filed"
	EvChatMessage         = "chat.message"         // Data: ChatMessage
	EvConversationUpdated = "conversation.updated" // Data: {"task_id"}
	EvChatDelta           = "chat.delta"           // Data: {"conversation_id","text"}; live
	EvGoalUpdated         = "goal.updated"         // Data: Goal
	EvLoopError           = "loop.error"
	EvNotification        = "notification"
	EvSystem              = "system"
)

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
)

// Approval is a decision onett is waiting for you to make.
type Approval struct {
	ID      string `json:"id"`
	TaskID  string `json:"task_id"`
	Project string `json:"project"`
	Kind    string `json:"kind"` // "merge", "unblock"
	Title   string `json:"title"`
	Reason  string `json:"reason"`
	// HeadSHA is the PR head the approval is for; a different head needs a new approval.
	HeadSHA   string         `json:"head_sha,omitempty"`
	Status    ApprovalStatus `json:"status"`
	Note      string         `json:"note,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	DecidedAt *time.Time     `json:"decided_at,omitempty"`
}

type DeployState string

const (
	DeployPending    DeployState = "pending"
	DeployDeploying  DeployState = "deploying"
	DeployVerifying  DeployState = "verifying"
	DeployHealthy    DeployState = "healthy"
	DeployFailed     DeployState = "failed"
	DeployRolledBack DeployState = "rolled_back"
)

// Deployment is onett's record of one release of a project.
type Deployment struct {
	ID         string      `json:"id"`
	Project    string      `json:"project"`
	TaskID     string      `json:"task_id,omitempty"`
	Ref        string      `json:"ref"`         // commit SHA
	ProviderID string      `json:"provider_id"` // the deployer's own ID
	URL        string      `json:"url,omitempty"`
	State      DeployState `json:"state"`
	Notes      string      `json:"notes,omitempty"`
	// VerifyingSince is when the release became ready and its health window started.
	VerifyingSince *time.Time `json:"verifying_since,omitempty"`
	// Log is evidence gathered for this release: the deploy output tail and, on failure, the
	// tail of the project's deploy.logs command.
	Log       string    `json:"log,omitempty"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SignalObservation is what an observer reports about one production problem.
type SignalObservation struct {
	Fingerprint string    `json:"fingerprint"`
	Title       string    `json:"title"`
	Level       string    `json:"level"` // "error", "warning", "fatal"
	Count       int64     `json:"count"`
	URL         string    `json:"url,omitempty"`
	Culprit     string    `json:"culprit,omitempty"`
	Release     string    `json:"release,omitempty"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// Signal is a deduplicated production problem, keyed by project, source and fingerprint.
type Signal struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Source  string `json:"source"` // observer name
	SignalObservation
	TaskID    string    `json:"task_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Friction is something that slowed an agent down, deduplicated by fingerprint.
type Friction struct {
	ID          string `json:"id"`
	Project     string `json:"project"` // the project whose repo should get the issue
	RunID       string `json:"run_id,omitempty"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	IssueURL    string `json:"issue_url,omitempty"`
	// IssueState is the filed issue's state as the friction loop last saw it: "open" or
	// "closed"; empty until known.
	IssueState string    `json:"issue_state,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SourceConversation is Task.Source for a follow-up task made from a task agent conversation
// after the task has merged.
const SourceConversation = "conversation"

// SourceChatPrefix starts Task.Source for a task the orchestrator created from a chat
// conversation; the rest is the conversation ID ("chat:<id>"). Policy rules that match sources
// ("followup", "retro") compare whole strings, so they are unaffected.
const SourceChatPrefix = "chat:"

// ChatConversationID returns the chat conversation a task is linked to, or "". A task the
// orchestrator created has it in Source; a follow-up of such a task keeps Source "followup" (policy
// rules match it) and carries the link in SourceRef instead.
func (t Task) ChatConversationID() string {
	for _, s := range []string{t.Source, t.SourceRef} {
		if id, ok := strings.CutPrefix(s, SourceChatPrefix); ok && id != "" {
			return id
		}
	}
	return ""
}

// Conversation is one chat thread: with the orchestrator agent, or (when TaskID is set) with
// the agent of one task.
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Project   string    `json:"project,omitempty"` // default project for delegated work
	SessionID string    `json:"session_id,omitempty"`
	Busy      bool      `json:"busy"` // the orchestrator is replying
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// TaskID is the root task of a task agent conversation; "" means the orchestrator.
	TaskID string `json:"task_id,omitempty"`
	// ActiveRunID is the orchestrator turn in flight.
	ActiveRunID string     `json:"active_run_id,omitempty"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	// ClosedBy is "agent", "orchestrator" or "owner:<username>".
	ClosedBy     string `json:"closed_by,omitempty"`
	ClosedReason string `json:"closed_reason,omitempty"`
	// GoalID is set on the conversation a goal runs in; its orchestrator turns get ONETT_GOAL.
	GoalID string `json:"goal_id,omitempty"`
}

type ChatMessage struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"` // "user", "assistant", "system"
	Text           string    `json:"text"`
	RunID          string    `json:"run_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	// Queued is an owner message held until the agent's current run finishes.
	Queued bool `json:"queued,omitempty"`
	// Attachments are the files sent with the message. When storing a message only the IDs
	// matter; the store fills in the rest on read. A file that retention has deleted is left out.
	Attachments []Attachment `json:"attachments,omitempty"`
	// Kind is "internal" for a prompt onett wrote to the orchestrator itself (a follow-up after
	// tasks settle, a goal check-in). Clients hide these from the owner and never forward them to a
	// chat app. Empty for every ordinary message.
	Kind string `json:"kind,omitempty"`
}

// Attachment is a file the owner sent in chat. The bytes live on disk under the data directory;
// Path is that location and is never shown through the API.
type Attachment struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Name           string    `json:"name"` // sanitised file name, not a path
	MIME           string    `json:"mime"` // sniffed from the content, never from the client
	Size           int64     `json:"size"`
	CreatedAt      time.Time `json:"created_at"`
	Path           string    `json:"-"`
}

// ChatKindInternal marks a ChatMessage as onett's own prompt to an agent, not conversation.
const ChatKindInternal = "internal"

// TaskConversation is the owner's view of a task agent: the root task, its conversation, the
// follow-up tasks and runs, and the messages.
type TaskConversation struct {
	Task         Task          `json:"task"` // the root task
	Conversation *Conversation `json:"conversation,omitempty"`
	Tasks        []Task        `json:"tasks"` // root then conversation follow-ups, oldest first
	Runs         []Run         `json:"runs"`  // the agent's runs across Tasks, oldest first
	Messages     []ChatMessage `json:"messages"`
	LiveRunID    string        `json:"live_run_id,omitempty"`
	CanSend      bool          `json:"can_send"`
	Reason       string        `json:"reason,omitempty"` // why CanSend is false
}

// AgentConversation is one row in the list of task agent conversations.
type AgentConversation struct {
	TaskID     string     `json:"task_id"`
	Title      string     `json:"title"`
	Project    string     `json:"project"`
	Role       string     `json:"role"`
	TaskStatus TaskStatus `json:"task_status"`
	Live       bool       `json:"live"`
	Closed     bool       `json:"closed"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// LoopState is the persisted state of one recurring loop for one project ("" = global).
type LoopState struct {
	Name      string    `json:"name"`
	Project   string    `json:"project"`
	LastRunAt time.Time `json:"last_run_at"`
	LastError string    `json:"last_error,omitempty"`
	Cursor    string    `json:"cursor,omitempty"` // loop-specific resume point
}

// Pause reasons: why the factory is paused.
const (
	PauseOwner      = "owner"       // a person (or their CLI) pressed pause
	PauseSelfUpdate = "self-update" // the updater is draining runs to swap the binary
)

// PauseInfo says why the global pause switch is on, who set it and since when. Detail is
// reason-specific: for a self-update, the commit being installed.
type PauseInfo struct {
	Reason string    `json:"reason"`
	By     string    `json:"by,omitempty"`
	Detail string    `json:"detail,omitempty"`
	Since  time.Time `json:"since"`
}

type GoalStatus string

const (
	GoalActive  GoalStatus = "active"
	GoalPaused  GoalStatus = "paused"
	GoalBlocked GoalStatus = "blocked"
	GoalDone    GoalStatus = "done"
)

// Goal is an objective the owner stated once. While it is active, the goals loop wakes the
// orchestrator in the goal's own conversation each time the previous step has settled.
type Goal struct {
	ID      string     `json:"id"`
	Project string     `json:"project"`
	Title   string     `json:"title"`
	Body    string     `json:"body"`
	Status  GoalStatus `json:"status"`
	// ConversationID is the orchestrator conversation the goal's turns run in.
	ConversationID string `json:"conversation_id"`
	// Turns counts the orchestrator turns the loop has started; LastTurnAt is when the last began.
	Turns      int        `json:"turns"`
	LastTurnAt *time.Time `json:"last_turn_at,omitempty"`
	// StalledTurns counts consecutive turns that made no progress. One earns a second chance; the
	// next blocks the goal. A turn with progress resets it.
	StalledTurns int `json:"stalled_turns,omitempty"`
	// Summary is why the goal is done or blocked.
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HarnessHold says new runs on a harness are held because it hit a subscription or provider
// rate limit. The hold lifts by itself at Until.
type HarnessHold struct {
	Harness string    `json:"harness"`
	Until   time.Time `json:"until"`
	// Message is the harness's own words about the limit.
	Message string `json:"message,omitempty"`
	// Hits counts consecutive limit hits on this harness; it sets the backoff when the harness
	// gave no reset time.
	Hits int `json:"hits"`
}
