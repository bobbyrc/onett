package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// AgentToken returns the token handed to the orchestrator agent. It is derived from the owner
// token (the secret in <data_dir>/token) with a keyed hash, so nothing new is stored, it is the
// same after a restart, and knowing it does not reveal the owner token. An empty owner token
// gives an empty agent token, which never matches a request.
func AgentToken(ownerToken string) string {
	return deriveFromToken(ownerToken, "onett agent token v1")
}

// SetupCode returns the code that proves the first account is being created by the operator:
// `onett open` puts it in the address it opens. It is derived the same way as AgentToken, under
// a different label, so the two are unrelated values.
func SetupCode(ownerToken string) string {
	return deriveFromToken(ownerToken, "onett setup code v1")
}

// deriveFromToken is HMAC-SHA256 of label keyed by the owner token, in hex.
func deriveFromToken(ownerToken, label string) string {
	if ownerToken == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(ownerToken))
	mac.Write([]byte(label))
	return hex.EncodeToString(mac.Sum(nil))
}

// PersonAction is a decision that belongs to a person. The agent token is refused each one
// listed in agentRefused.
type PersonAction string

const (
	// ApproveMerge is approving or rejecting an approval.
	ApproveMerge PersonAction = "approve_merge"
	// ResumeFactory is clearing the factory pause.
	ResumeFactory PersonAction = "resume_factory"
	// ResumeProject is clearing a project pause.
	ResumeProject PersonAction = "resume_project"
	// ResumePausedGoal is resuming a goal that someone paused.
	ResumePausedGoal PersonAction = "resume_paused_goal"
	// RunLoop is running a loop by hand, which also runs a loop that is disabled.
	RunLoop PersonAction = "run_loop"
	// ResetGoalBudget is giving a blocked goal a fresh turn budget when it is resumed.
	ResetGoalBudget PersonAction = "reset_goal_budget"
	// WriteConversation is writing in an orchestrator conversation, where text is the owner's.
	WriteConversation PersonAction = "write_conversation"
	// RetryDenied is retrying, or messaging, a task that was blocked because a tool call was denied.
	RetryDenied PersonAction = "retry_denied"
)

// agentRefused is the one place that says what the agent token may not do, with the reason the
// agent is given so it can tell the owner what to do instead of trying another way. Agents may
// stop things (pause, cancel, block); starting them again and deciding are for a person. To
// let the agent token do one of these again, delete its line and the test that names it.
var agentRefused = map[PersonAction]string{
	ApproveMerge:      "approving or rejecting a merge is the owner's decision; tell the owner to use the approval card in the web UI or their chat channel, or to run `onett approve` in their own terminal",
	ResumeFactory:     "resuming the factory is the owner's decision; tell the owner to resume it in the web UI or to run `onett resume` in their own terminal",
	ResumeProject:     "resuming a project is the owner's decision; tell the owner to resume it on the Projects page or to run `onett project resume` in their own terminal",
	ResumePausedGoal:  "resuming a paused goal is the owner's decision; tell the owner to resume it on the goal's page or to run `onett goal resume` in their own terminal",
	RunLoop:           "running a loop by hand is the owner's decision; tell the owner to run it on the Loops page or with `onett loop run` in their own terminal",
	ResetGoalBudget:   "a fresh turn budget for a blocked goal is the owner's decision",
	WriteConversation: "only the owner writes in an orchestrator conversation",
	RetryDenied:       "this task was blocked because a tool call was denied, and only the owner may retry a denial; tell the owner to adjust the permissions or the task and retry it",
}

type agentKey struct{}

// WithAgent marks ctx as a request made with the agent token. The API's sign-in middleware is
// the only caller, so every API request from the agent carries the mark.
func WithAgent(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentKey{}, true)
}

// IsAgent reports whether ctx is a request made with the agent token. Code that onett runs
// itself (loops, chat-channel buttons) has no mark and is not an agent.
func IsAgent(ctx context.Context) bool {
	v, _ := ctx.Value(agentKey{}).(bool)
	return v
}

// ErrPersonOnly marks a refusal of the agent token. The API answers it with 403.
var ErrPersonOnly = errors.New("only a person can do this")

// Refused returns nil unless ctx is a request made with the agent token and a is in
// agentRefused; then it returns an error that wraps ErrPersonOnly and carries the reason.
func Refused(ctx context.Context, a PersonAction) error {
	if !IsAgent(ctx) {
		return nil
	}
	reason, ok := agentRefused[a]
	if !ok {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPersonOnly, reason)
}
