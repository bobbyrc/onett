package api

import (
	"context"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	agentharness "github.com/bobbyrc/onett/internal/harness"
)

// SettingsView is a conversation's agent settings with what the UI needs to offer them.
type SettingsView struct {
	Settings domain.ConversationSettings `json:"settings"`
	// Defaults are the role's values for what Settings leaves empty (the agent's model,
	// effort and permission mode; empty means the harness's own default).
	Defaults domain.ConversationSettings `json:"defaults"`
	Harness  string                      `json:"harness"`
	Choices  agentharness.Choices        `json:"choices"`
}

// Controller is everything the API does beyond reading the store. internal/engine implements
// it. Errors wrapping store.ErrNotFound map to 404, ErrInvalid to 400, ErrConflict to 409.
type Controller interface {
	// CreateTask validates the project, fills Role from routing when empty, sets MaxAttempts
	// from limits, queues the task and wakes the scheduler.
	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	// CancelTask cancels the task. by names who asked ("orchestrator" or "owner:<username>"); it is
	// recorded as the closer of the task agent's conversation.
	CancelTask(ctx context.Context, id, by string) error
	// RetryTask re-queues a blocked, failed or cancelled task with Attempts reset to 0.
	// A non-empty note is appended to the task body as human guidance.
	RetryTask(ctx context.Context, id, note string) error
	CancelRun(ctx context.Context, id string) error
	// CreateGoal validates the project, creates the goal and its conversation, and returns the
	// active goal. Its first orchestrator turn happens on the goals loop's next tick.
	CreateGoal(ctx context.Context, g domain.Goal) (domain.Goal, error)
	// SetGoalStatus moves a goal to paused, active (resume), done or blocked. text is the summary
	// for done or the reason for blocked, and is required for both. ErrConflict when the move is
	// not allowed from the goal's current status. Done and blocked notify the owner.
	SetGoalStatus(ctx context.Context, id string, status domain.GoalStatus, text string) (domain.Goal, error)
	// TaskConversation returns the task agent conversation that taskID belongs to: the root
	// task, follow-up tasks, the agent's runs, messages, and whether the owner can message it.
	TaskConversation(ctx context.Context, taskID string) (domain.TaskConversation, error)
	// AgentConversations lists the task agents the owner can talk to, open ones first.
	AgentConversations(ctx context.Context) ([]domain.AgentConversation, error)
	// SendTaskMessage sends text to the agent that works on taskID. A live agent that takes input
	// gets it at once; otherwise it is queued and the agent resumes its session with it. author is
	// stored on the message: "" for the owner, domain.AuthorOrchestrator for the API token.
	// ErrConflict, with the reason, when the agent cannot be messaged.
	SendTaskMessage(ctx context.Context, taskID, text, author string) (domain.ChatMessage, error)
	// FlushTaskMessages starts the follow-up for queued messages now, instead of waiting for the
	// agent's run to succeed. It does nothing when no message is queued.
	FlushTaskMessages(ctx context.Context, taskID string) error
	// CloseTaskConversation closes the task agent's conversation (ErrConflict when it is already
	// closed); ReopenTaskConversation opens it again (ErrConflict when it is open). Neither
	// changes the task.
	CloseTaskConversation(ctx context.Context, taskID, by, reason string) error
	ReopenTaskConversation(ctx context.Context, taskID string) error
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)

	Loops(ctx context.Context) []LoopView
	// RunLoop runs one loop now; project "" runs it for every project.
	RunLoop(ctx context.Context, name, project string) error

	// Preflight is the last tool-directory check per project (see internal/engine/preflight.go).
	Preflight() []domain.Preflight
	// Holds lists the harnesses held for a rate limit, with the time each hold lifts.
	Holds() []domain.HarnessHold

	Config() *config.Config
	ReloadConfig(ctx context.Context) error

	// Paused reports the global pause switch. While paused no run starts and no loop acts.
	Paused() bool
	SetPaused(ctx context.Context, paused bool) error
	// PauseWith pauses the factory and records why. The reason must be domain.PauseOwner
	// (ErrInvalid otherwise).
	PauseWith(ctx context.Context, info domain.PauseInfo) error
	// PauseInfo reports why the factory is paused, or nil when it is running.
	PauseInfo() *domain.PauseInfo
	// ProjectPausedBy reports why a project is paused: "config" (paused: true in onett.yaml,
	// which the API cannot change), "owner" (paused at runtime through the API) or "".
	ProjectPausedBy(project string) string
	// SetProjectPaused pauses or resumes one project at runtime (persisted across restarts).
	// ErrInvalid for an unknown project; resuming a project paused in config is ErrConflict.
	SetProjectPaused(ctx context.Context, project string, paused bool) error

	// SendChat appends a user message. Idle, it starts the orchestrator's reply, which arrives
	// as chat.delta events and a final chat.message event. While a reply is running the message
	// goes straight to an agent that takes input, or else is queued for the next turn.
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
	// SendChatWith is SendChat for a message that carries attachments (IDs from the attachment
	// upload). Text may be empty when there are any. They are stored with the message.
	SendChatWith(ctx context.Context, conversationID, text string, attachmentIDs []string) (domain.ChatMessage, error)
	// ConversationSettings returns a conversation's model, effort and permission mode, the role's
	// defaults, and the choices its harness accepts.
	ConversationSettings(ctx context.Context, id string) (SettingsView, error)
	// UpdateConversationSettings replaces the settings, which apply from the next turn. Values
	// outside the harness's choices are ErrInvalid, as is a dangerous permission mode (one that
	// skips safety checks) chosen without confirmDangerous.
	UpdateConversationSettings(ctx context.Context, id string, in domain.ConversationSettings, confirmDangerous bool) (SettingsView, error)
	// FlushChat starts a turn from the queued messages now. It does nothing when the queue is
	// empty or a turn is running.
	FlushChat(ctx context.Context, conversationID string) error
	// EditQueuedMessage and CancelQueuedMessage change a message not yet delivered. ErrConflict
	// once it has been; ErrNotFound for an unknown message.
	EditQueuedMessage(ctx context.Context, id, text string) (domain.ChatMessage, error)
	CancelQueuedMessage(ctx context.Context, id string) error

	// AgentPrompt is an agent's instructions: its inline PromptText, else the file or builtin
	// its Prompt names. BuiltinPrompt is the embedded prompt of one built-in agent.
	AgentPrompt(a config.Agent) (string, error)
	BuiltinPrompt(name string) (string, error)
}

type LoopView struct {
	Name      string    `json:"name"`
	Project   string    `json:"project"`
	Enabled   bool      `json:"enabled"`
	Every     string    `json:"every"`
	LastRunAt time.Time `json:"last_run_at"`
	LastError string    `json:"last_error,omitempty"`
	// NextRunAt and ItemCount are set only by loops that report them (heartbeat).
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	ItemCount int        `json:"item_count,omitempty"`
	// The rest is set only by the heartbeat loop: actions and escalations in the last 24 hours,
	// the Operations conversation ("" until it exists) and whether quiet hours are on now.
	Actions24h               int    `json:"actions_24h,omitempty"`
	Escalations24h           int    `json:"escalations_24h,omitempty"`
	OperationsConversationID string `json:"operations_conversation_id,omitempty"`
	QuietNow                 bool   `json:"quiet_now,omitempty"`
}
