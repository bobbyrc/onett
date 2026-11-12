package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
)

// tbBlockedTask makes a task in goalID with the given status, and a run of it with runStatus when
// runStatus is not "". A blocked task whose latest run was denied is one the agent may not retry.
func tbBlockedTask(t *testing.T, r *ctRig, e *Engine, goalID string, status domain.TaskStatus, runStatus domain.RunStatus) domain.Task {
	t.Helper()
	ctx := context.Background()
	task, err := e.CreateTask(ctx, domain.Task{Project: "demo", Kind: domain.KindChore, Title: "step", GoalID: goalID})
	require.NoError(t, err)
	task, err = r.st.UpdateTask(ctx, task.ID, func(x *domain.Task) error {
		x.Status = status
		return nil
	})
	require.NoError(t, err)
	if runStatus != "" {
		// The run has a harness session, as a run the agent started and was denied in does.
		_, err := r.st.CreateRun(ctx, domain.Run{TaskID: task.ID, Project: "demo", Role: task.Role, Status: runStatus, SessionID: "s", StartedAt: time.Now()})
		require.NoError(t, err)
	}
	return task
}

func TestMessageHeadingNamesTheAuthor(t *testing.T) {
	assert.Equal(t, "## Message from the owner\n\nhi", messagesPrompt([]domain.ChatMessage{{Text: "hi"}}))
	assert.Equal(t, "## Message from the orchestrator\n\nhi",
		messagesPrompt([]domain.ChatMessage{{Text: "hi", Author: domain.AuthorOrchestrator}}))
	assert.Equal(t, "## Message from the owner\n\na\n\nb\n\n## Message from the orchestrator\n\nc",
		messagesPrompt([]domain.ChatMessage{{Text: "a"}, {Text: "b"}, {Text: "c", Author: domain.AuthorOrchestrator}}))
}

func TestAgentCannotResumePausedGoal(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	goal, err := e.CreateGoal(ctx, domain.Goal{Project: "demo", Title: "Ship parts", Body: "part 1"})
	require.NoError(t, err)
	_, err = e.SetGoalStatus(ctx, goal.ID, domain.GoalPaused, "")
	require.NoError(t, err)

	_, err = e.SetGoalStatus(agentCtx, goal.ID, domain.GoalActive, "")
	assert.True(t, errors.Is(err, auth.ErrPersonOnly), "got %v", err)
	got, err := r.st.GetGoal(ctx, goal.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GoalPaused, got.Status)

	_, err = e.SetGoalStatus(ctx, goal.ID, domain.GoalActive, "")
	require.NoError(t, err)
	got, err = r.st.GetGoal(ctx, goal.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GoalActive, got.Status)
}

func TestAgentResumeKeepsBlockedGoalBudget(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	goal, err := e.CreateGoal(ctx, domain.Goal{Project: "demo", Title: "Ship parts", Body: "part 1"})
	require.NoError(t, err)
	_, err = r.st.UpdateGoal(ctx, goal.ID, func(g *domain.Goal) error {
		g.Turns, g.StalledTurns = 5, 1
		return nil
	})
	require.NoError(t, err)

	_, err = e.SetGoalStatus(ctx, goal.ID, domain.GoalBlocked, "needs the owner")
	require.NoError(t, err)
	_, err = e.SetGoalStatus(agentCtx, goal.ID, domain.GoalActive, "")
	require.NoError(t, err)
	got, err := r.st.GetGoal(ctx, goal.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GoalActive, got.Status)
	assert.Equal(t, 5, got.Turns, "the agent resumes with the budget it has left")
	assert.Equal(t, 1, got.StalledTurns)

	_, err = e.SetGoalStatus(ctx, goal.ID, domain.GoalBlocked, "needs the owner")
	require.NoError(t, err)
	_, err = e.SetGoalStatus(ctx, goal.ID, domain.GoalActive, "")
	require.NoError(t, err)
	got, err = r.st.GetGoal(ctx, goal.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, got.Turns, "a person resuming a blocked goal gets a fresh budget")
	assert.Equal(t, 0, got.StalledTurns)
}

func TestAgentCannotRetryDeniedTask(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	task := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunDenied)

	err := e.RetryTask(agentCtx, task.ID, "try again")
	assert.True(t, errors.Is(err, auth.ErrPersonOnly), "got %v", err)
	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, got.Status)

	require.NoError(t, e.RetryTask(ctx, task.ID, "fixed the permission"))
	got, err = r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskQueued, got.Status)
	assert.Contains(t, got.Body, "## Note from the owner")
}

func TestAgentRetriesOrdinaryBlockedTask(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	task := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunFailed)

	require.NoError(t, e.RetryTask(agentCtx, task.ID, "try X"))
	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskQueued, got.Status)
	assert.Contains(t, got.Body, "## Note from the orchestrator")
	assert.NotContains(t, got.Body, "## Note from the owner")
}

func TestAgentCannotMessageDeniedTask(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	task := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunDenied)

	_, err := e.SendTaskMessage(agentCtx, task.ID, "go on", domain.AuthorOrchestrator)
	assert.True(t, errors.Is(err, auth.ErrPersonOnly), "got %v", err)
	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, got.Status)

	_, err = e.SendTaskMessage(ctx, task.ID, "go on", "")
	assert.NoError(t, err)
}

func TestDeniedTaskInGoalNotifiesOwner(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	goal, err := e.CreateGoal(ctx, domain.Goal{Project: "demo", Title: "Ship parts", Body: "part 1"})
	require.NoError(t, err)

	denied := tbBlockedTask(t, r, e, goal.ID, domain.TaskBlocked, domain.RunDenied)
	assert.False(t, e.goalHandlesBlock(ctx, denied), "a denial is the owner's to retry")

	ordinary := tbBlockedTask(t, r, e, goal.ID, domain.TaskBlocked, domain.RunFailed)
	assert.True(t, e.goalHandlesBlock(ctx, ordinary), "an ordinary block is the active goal's to handle")
}

func TestAgentCannotRetryDeniedTaskAfterCancellingIt(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	task := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunDenied)

	require.NoError(t, e.CancelTask(agentCtx, task.ID, "orchestrator"), "cancelling is allowed")
	err := e.RetryTask(agentCtx, task.ID, "again")
	assert.True(t, errors.Is(err, auth.ErrPersonOnly), "got %v", err)
	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskCancelled, got.Status)

	require.NoError(t, e.RetryTask(ctx, task.ID, "fixed"))
	got, err = r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskQueued, got.Status)
}

func TestAgentCannotBlockOrFinishPausedGoal(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	goal, err := e.CreateGoal(ctx, domain.Goal{Project: "demo", Title: "Ship parts", Body: "part 1"})
	require.NoError(t, err)
	_, err = e.SetGoalStatus(ctx, goal.ID, domain.GoalPaused, "")
	require.NoError(t, err)

	_, err = e.SetGoalStatus(agentCtx, goal.ID, domain.GoalBlocked, "needs the owner")
	assert.True(t, errors.Is(err, auth.ErrPersonOnly), "got %v", err)
	got, err := r.st.GetGoal(ctx, goal.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GoalPaused, got.Status)

	_, err = e.SetGoalStatus(agentCtx, goal.ID, domain.GoalDone, "finished")
	assert.True(t, errors.Is(err, auth.ErrPersonOnly), "got %v", err)
	got, err = r.st.GetGoal(ctx, goal.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GoalPaused, got.Status)

	_, err = e.SetGoalStatus(ctx, goal.ID, domain.GoalBlocked, "needs the owner")
	require.NoError(t, err)
	got, err = r.st.GetGoal(ctx, goal.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GoalBlocked, got.Status)
}

// tbReviewOf makes a review task under parent with the given status and CreatedAt, and a run of
// it with runStatus. CreateTask refuses review tasks, so the store writes it directly.
func tbReviewOf(t *testing.T, r *ctRig, parent domain.Task, status domain.TaskStatus, runStatus domain.RunStatus, createdAt time.Time) domain.Task {
	t.Helper()
	ctx := context.Background()
	review, err := r.st.CreateTask(ctx, domain.Task{Project: "demo", Kind: domain.KindReview, Title: "review", Role: parent.Role,
		ParentID: parent.ID, Status: status, CreatedAt: createdAt})
	require.NoError(t, err)
	_, err = r.st.CreateRun(ctx, domain.Run{TaskID: review.ID, Project: "demo", Role: review.Role, Status: runStatus, SessionID: "s", StartedAt: createdAt})
	require.NoError(t, err)
	return review
}

func TestClientCannotCreateConversationFollowUp(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	parent, err := e.CreateTask(ctx, domain.Task{Project: "demo", Kind: domain.KindFeature, Title: "p", Body: "q"})
	require.NoError(t, err)

	followUp := domain.Task{Project: "demo", Kind: domain.KindFeature, Title: "x", Body: "y", ParentID: parent.ID, Source: domain.SourceConversation}
	_, err = e.CreateTask(ctx, followUp)
	assert.True(t, errors.Is(err, api.ErrInvalid), "got %v", err)

	followUp.Source = ""
	_, err = e.CreateTask(ctx, followUp)
	assert.NoError(t, err)
}

func TestAgentCannotRetryTaskWhoseReviewWasDenied(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	parent := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunSucceeded)
	tbReviewOf(t, r, parent, domain.TaskBlocked, domain.RunDenied, time.Now())

	err := e.RetryTask(agentCtx, parent.ID, "again")
	assert.True(t, errors.Is(err, auth.ErrPersonOnly), "got %v", err)
	got, err := r.st.GetTask(ctx, parent.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, got.Status)

	require.NoError(t, e.RetryTask(ctx, parent.ID, "fixed"))
}

func TestAgentRetriesTaskWhoseReviewEndedNormally(t *testing.T) {
	agentCtx := auth.WithAgent(context.Background())
	r := ctNewRig(t)
	e := r.engine(nil)
	parent := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunSucceeded)
	tbReviewOf(t, r, parent, domain.TaskDone, domain.RunSucceeded, time.Now())

	require.NoError(t, e.RetryTask(agentCtx, parent.ID, "try X"))
}

func TestOlderDeniedReviewDoesNotCount(t *testing.T) {
	agentCtx := auth.WithAgent(context.Background())
	r := ctNewRig(t)
	e := r.engine(nil)
	parent := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunSucceeded)
	tbReviewOf(t, r, parent, domain.TaskCancelled, domain.RunDenied, time.Now().Add(-time.Hour))
	tbReviewOf(t, r, parent, domain.TaskDone, domain.RunSucceeded, time.Now())

	require.NoError(t, e.RetryTask(agentCtx, parent.ID, ""))
}

func TestAgentMessageDoesNotRequeueDeniedTaskAfterTheCheck(t *testing.T) {
	ctx := context.Background()
	agentCtx := auth.WithAgent(ctx)
	r := ctNewRig(t)
	e := r.engine(nil)
	task := tbBlockedTask(t, r, e, "", domain.TaskBlocked, domain.RunDenied)
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{TaskID: task.ID, Title: task.Title, Project: task.Project})
	require.NoError(t, err)
	_, err = r.st.AppendChatMessage(ctx, domain.ChatMessage{ConversationID: conv.ID, Role: "user", Text: "go on", Queued: true, Author: domain.AuthorOrchestrator})
	require.NoError(t, err)

	e.continueConversation(agentCtx, task.ID)
	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, got.Status, "the agent's queued message must not re-queue a denial")

	e.continueConversation(ctx, task.ID)
	got, err = r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskQueued, got.Status)
}
