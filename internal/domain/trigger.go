package domain

import "time"

// TriggerKind says what makes a trigger fire.
type TriggerKind string

const (
	// TriggerSchedule fires at the times its cron expression names.
	TriggerSchedule TriggerKind = "schedule"
	// TriggerGitHubIssue fires once for each new issue opened in the project's repository.
	TriggerGitHubIssue TriggerKind = "github_issue"
)

// Firing causes: why a firing happened.
const (
	FiringCauseSchedule    = "schedule"
	FiringCauseGitHubIssue = "github_issue"
	FiringCauseManual      = "manual"
)

// Firing outcomes. Started means the prompt was handed to the orchestrator: its turn started,
// or the prompt is queued behind the turn already running in the trigger's conversation.
const (
	FiringStarted = "started"
	FiringSkipped = "skipped"
	FiringFailed  = "failed"
)

// Trigger events.
const (
	EvTriggerUpdated = "trigger.updated" // Data: Trigger
	EvTriggerDeleted = "trigger.deleted" // Data: {"id"}
	EvTriggerFired   = "trigger.fired"   // Data: TriggerFiring
)

// ConversationTrigger is the Kind of the conversation a trigger's turns run in. Every turn in it
// runs with the orchestrator role's default settings: settings stored on it are not applied.
const ConversationTrigger = "trigger"

// Limits on the owner-written parts of a trigger, in characters.
const (
	TriggerNameMax   = 100
	TriggerPromptMax = 20000
)

// Trigger is an owner-defined rule that starts one orchestrator turn with Prompt. Only a
// signed-in owner may create, change or fire one; see docs/proposals/triggers.md.
type Trigger struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	Kind TriggerKind `json:"kind"` // fixed after creation
	// Project is the default project of the trigger's conversation, the scope of the project
	// pause, and for a github_issue trigger the repository that is watched. Fixed after creation.
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
	Enabled bool   `json:"enabled"` // a disabled trigger never fires by itself

	// Cron and TimeZone are for a schedule trigger: a five-field cron expression and an IANA
	// zone name ("" is the daemon's local zone). NextRunAt is when it fires next; nil while
	// disabled.
	Cron      string     `json:"cron,omitempty"`
	TimeZone  string     `json:"time_zone,omitempty"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`

	// Label and IssuesAfter are for a github_issue trigger. Label, when set, limits it to issues
	// that carry the label. Only issues created after IssuesAfter can fire; it is set when the
	// trigger is created and again each time it is enabled.
	Label       string     `json:"label,omitempty"`
	IssuesAfter *time.Time `json:"issues_after,omitempty"`

	// ConversationID is the conversation its turns run in, set at the first firing.
	ConversationID string    `json:"conversation_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TriggerPatch is a partial update of a trigger: a nil field is left unchanged. Kind and Project
// cannot be changed.
type TriggerPatch struct {
	Name     *string `json:"name,omitempty"`
	Prompt   *string `json:"prompt,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
	Cron     *string `json:"cron,omitempty"`
	TimeZone *string `json:"time_zone,omitempty"`
	Label    *string `json:"label,omitempty"`
}

// TriggerFiring records one time a trigger fired or would have: its cause, its outcome and,
// when it started, where the prompt went.
type TriggerFiring struct {
	ID        string    `json:"id"`
	TriggerID string    `json:"trigger_id"`
	At        time.Time `json:"at"`    // when onett evaluated it
	Cause     string    `json:"cause"` // a FiringCause constant
	// ScheduledFor is the scheduled time a schedule firing answers.
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	// IssueNumber and IssueURL name the issue of a github_issue firing.
	IssueNumber int    `json:"issue_number,omitempty"`
	IssueURL    string `json:"issue_url,omitempty"`
	// By is who asked for a manual firing: "owner:<username>".
	By      string `json:"by,omitempty"`
	Outcome string `json:"outcome"`          // FiringStarted, FiringSkipped or FiringFailed
	Reason  string `json:"reason,omitempty"` // why it was skipped or failed
	// ConversationID and MessageID are the conversation and the internal message that carried
	// the prompt of a started firing. That message's RunID names the run once the turn starts.
	ConversationID string `json:"conversation_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
}
