package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
)

// pbYAML is two projects, alpha and beta, with prod_watch off so a stale error for it is ignored.
const pbYAML = `server:
  data_dir: %[1]s
loops:
  prod_watch:
    enabled: false
projects:
  alpha:
    repo: bobbyrc/alpha
    path: %[2]s
  beta:
    repo: bobbyrc/beta
    path: %[3]s
`

func pbEnv(t *testing.T) *lpEnv {
	t.Helper()
	dir := t.TempDir()
	return lpNewEngine(t, fmt.Sprintf(pbYAML, t.TempDir(), filepath.Join(dir, "alpha"), filepath.Join(dir, "beta")))
}

func pbAt(t time.Time) *time.Time { return &t }

// pbProblems returns the problems of one kind in one project.
func pbProblems(t *testing.T, env *lpEnv, kind, project string) []api.Problem {
	t.Helper()
	all, err := env.e.Problems(env.ctx)
	require.NoError(t, err)
	var out []api.Problem
	for _, p := range all {
		if p.Kind == kind && p.Project == project {
			out = append(out, p)
		}
	}
	return out
}

// pbLoopError saves a loop state of project with an error from lastRun.
func pbLoopError(t *testing.T, env *lpEnv, loop, project, msg string, lastRun time.Time) {
	t.Helper()
	require.NoError(t, env.st.SaveLoopState(env.ctx, domain.LoopState{Name: loop, Project: project, LastRunAt: lastRun, LastError: msg}))
}

func TestProblemsShowOneCauseOnce(t *testing.T) {
	env := pbEnv(t)
	base := env.now
	for i := range 7 {
		lpRetroTask(t, env, domain.Task{
			Project: "alpha", Title: fmt.Sprintf("setup %d", i), Status: domain.TaskBlocked,
			BlockedReason: "workspace setup failed (gave up after 3 attempts)", BlockedCause: "workspace setup failed",
			BlockedDetail: fmt.Sprintf("fetch failed %d", i), BlockedAt: pbAt(base.Add(-time.Duration(i+1) * time.Hour)),
		})
	}
	lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "ask", Status: domain.TaskBlocked,
		BlockedReason: "agent needs a human: which colour?", BlockedAt: pbAt(base.Add(-2 * time.Hour))})
	lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "busy", Status: domain.TaskRunning})

	got := pbProblems(t, env, api.ProblemBlockedTasks, "alpha")
	require.Len(t, got, 2)
	assert.Equal(t, 7, got[0].Count)
	assert.Equal(t, "workspace setup failed", got[0].Title)
	assert.Equal(t, "fetch failed 0", got[0].Detail)
	assert.True(t, got[0].Since.Equal(base.Add(-7*time.Hour)))
	require.NotNil(t, got[0].LastAt)
	assert.True(t, got[0].LastAt.Equal(base.Add(-time.Hour)))
	require.Len(t, got[0].Tasks, 7)
	assert.Equal(t, "setup 0", got[0].Tasks[0].Title, "most recently blocked first")
	assert.Equal(t, 1, got[1].Count)
	assert.Equal(t, "agent needs a human: which colour?", got[1].Title)
	assert.NotEqual(t, got[0].Key, got[1].Key)
}

func TestProblemsListAtMostFiftyTasks(t *testing.T) {
	env := pbEnv(t)
	for i := range 60 {
		lpRetroTask(t, env, domain.Task{
			Project: "alpha", Title: fmt.Sprintf("setup %d", i), Status: domain.TaskBlocked,
			BlockedCause: "workspace setup failed", BlockedReason: "workspace setup failed (gave up after 3 attempts)",
			BlockedAt: pbAt(env.now.Add(-time.Duration(i) * time.Minute)),
		})
	}
	got := pbProblems(t, env, api.ProblemBlockedTasks, "alpha")
	require.Len(t, got, 1)
	assert.Equal(t, 60, got[0].Count)
	assert.Len(t, got[0].Tasks, 50)
}

func TestProblemsUseUpdatedAtForOldBlocks(t *testing.T) {
	env := pbEnv(t)
	updated := env.now.Add(-3 * time.Hour)
	lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "old", Status: domain.TaskBlocked,
		BlockedReason: "old block", UpdatedAt: updated})

	got := pbProblems(t, env, api.ProblemBlockedTasks, "alpha")
	require.Len(t, got, 1)
	assert.True(t, got[0].Since.Equal(updated))
}

func TestProblemsReportFailingLoops(t *testing.T) {
	env := pbEnv(t)
	base := env.now
	pbLoopError(t, env, loopPRShepherd, "alpha", "pr shepherd: CI check exploded", base.Add(-time.Hour))
	pbLoopError(t, env, loopDeployWatch, "alpha", "deploy failed: exit status 1", base.Add(-10*time.Minute))
	pbLoopError(t, env, loopProdWatch, "alpha", "prod watch: disabled, must not show", base.Add(-time.Minute))
	pbLoopError(t, env, loopDeployWatch, "beta", "", base) // no error: no problem
	require.NoError(t, env.st.SaveLoopState(env.ctx, domain.LoopState{
		Name: loopAlertState + loopPRShepherd, Project: "alpha",
		Cursor: fmt.Sprintf(`{"fails":13,"since":%q,"notified":%q}`,
			base.Add(-5*time.Hour).Format(time.RFC3339Nano), base.Add(-30*time.Minute).Format(time.RFC3339Nano)),
	}))

	got := pbProblems(t, env, api.ProblemFailingLoops, "alpha")
	require.Len(t, got, 1)
	p := got[0]
	assert.Equal(t, "failing_loops:alpha", p.Key)
	assert.Empty(t, p.Title)
	assert.Equal(t, "deploy failed: exit status 1", p.Detail, "the most recent error")
	assert.Equal(t, 13, p.Count)
	assert.True(t, p.Since.Equal(base.Add(-5*time.Hour)))
	require.NotNil(t, p.LastAt)
	assert.True(t, p.LastAt.Equal(base.Add(-10*time.Minute)))
	require.NotNil(t, p.ToldAt)
	assert.True(t, p.ToldAt.Equal(base.Add(-30*time.Minute)))
	require.Len(t, p.Loops, 2)
	assert.Equal(t, api.ProblemLoop{Name: loopPRShepherd, Fails: 13, Since: base.Add(-5 * time.Hour)}, p.Loops[0])
	assert.Equal(t, api.ProblemLoop{Name: loopDeployWatch, Fails: 1, Since: base.Add(-10 * time.Minute)}, p.Loops[1])

	assert.Empty(t, pbProblems(t, env, api.ProblemFailingLoops, "beta"))
}

func TestProblemsReportBlockedGoals(t *testing.T) {
	env := pbEnv(t)
	base := env.now
	g, err := env.st.CreateGoal(env.ctx, domain.Goal{Project: "alpha", Title: "Ship the overview",
		Summary: "the deploy keeps failing", Status: domain.GoalBlocked, UpdatedAt: base.Add(-4 * time.Hour)})
	require.NoError(t, err)
	_, err = env.st.CreateGoal(env.ctx, domain.Goal{Project: "alpha", Title: "Still going", Status: domain.GoalActive})
	require.NoError(t, err)

	got := pbProblems(t, env, api.ProblemBlockedGoal, "alpha")
	require.Len(t, got, 1)
	p := got[0]
	assert.Equal(t, "blocked_goal:"+g.ID, p.Key)
	assert.Equal(t, g.ID, p.GoalID)
	assert.Equal(t, "Ship the overview", p.Title)
	assert.Equal(t, "the deploy keeps failing", p.Detail)
	assert.Equal(t, 1, p.Count)
	assert.True(t, p.Since.Equal(base.Add(-4*time.Hour)))
	assert.Nil(t, p.LastAt)
}

func TestProblemsSayWhenTheOwnerWasTold(t *testing.T) {
	env := pbEnv(t)
	base := env.now
	one := lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "one", Status: domain.TaskBlocked,
		BlockedReason: "reason one", BlockedAt: pbAt(base.Add(-time.Hour))})
	two := lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "two", Status: domain.TaskBlocked,
		BlockedReason: "reason two", BlockedAt: pbAt(base.Add(-2 * time.Hour))})
	told := base.Add(-20 * time.Minute)
	cursor := fmt.Sprintf(`{"items":{`+
		`"blocked_task:%[1]s:abcd1234":{"entity_id":%[1]q,"escalated":true,"escalated_at":%[2]q},`+
		`"blocked_task:%[3]s:efgh5678":{"entity_id":%[3]q,"escalated":true}}}`,
		one.ID, told.Format(time.RFC3339Nano), two.ID)
	require.NoError(t, env.st.SaveLoopState(env.ctx, domain.LoopState{Name: loopHeartbeat, Project: "alpha", Cursor: cursor}))

	got := pbProblems(t, env, api.ProblemBlockedTasks, "alpha")
	require.Len(t, got, 2)
	for _, p := range got {
		switch p.Title {
		case "reason one":
			require.NotNil(t, p.ToldAt)
			assert.True(t, p.ToldAt.Equal(told))
		case "reason two":
			assert.Nil(t, p.ToldAt, "escalated without a time: not told as far as the page knows")
		default:
			t.Fatalf("unexpected problem %q", p.Title)
		}
	}
}

func TestProblemsRedactAndBound(t *testing.T) {
	env := pbEnv(t)
	t.Setenv("ALPHA_DEPLOY_TOKEN", "alpha-secret-value-1")
	base := env.now
	reason := "before alpha-secret-value-1 happened\n" + strings.Repeat("word ", 200) + "\nlast line"
	lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "long", Status: domain.TaskBlocked,
		BlockedReason: reason, BlockedAt: pbAt(base.Add(-time.Hour))})
	detail := strings.Repeat("d", 1000-len("THE-END")) + "THE-END"
	lpRetroTask(t, env, domain.Task{Project: "alpha", Title: "detailed", Status: domain.TaskBlocked,
		BlockedReason: "detailed block", BlockedDetail: detail, BlockedAt: pbAt(base.Add(-2 * time.Hour))})

	got := pbProblems(t, env, api.ProblemBlockedTasks, "alpha")
	require.Len(t, got, 2)
	for _, p := range got {
		switch {
		case strings.HasPrefix(p.Title, "before "):
			assert.True(t, strings.HasPrefix(p.Title, "before *** happened word"), p.Title)
			assert.Len(t, []rune(p.Title), 200)
			assert.NotContains(t, p.Title, "\n")
			assert.NotContains(t, p.Title, "alpha-secret-value-1")
		case p.Title == "detailed block":
			r := []rune(p.Detail)
			assert.Len(t, r, 300)
			assert.True(t, strings.HasPrefix(p.Detail, "…"))
			assert.True(t, strings.HasSuffix(p.Detail, "THE-END"))
		default:
			t.Fatalf("unexpected problem %q", p.Title)
		}
	}
}

func TestProblemsAreEmptyNotNull(t *testing.T) {
	env := pbEnv(t)
	got, err := env.e.Problems(env.ctx)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestProblemsOrder(t *testing.T) {
	env := pbEnv(t)
	base := env.now
	_, err := env.st.CreateGoal(env.ctx, domain.Goal{Project: "alpha", Title: "Alpha goal",
		Summary: "stuck", Status: domain.GoalBlocked})
	require.NoError(t, err)
	for i := range 5 {
		lpRetroTask(t, env, domain.Task{Project: "beta", Title: fmt.Sprintf("five %d", i), Status: domain.TaskBlocked,
			BlockedCause: "beta five", BlockedReason: "beta five (gave up after 3 attempts)", BlockedAt: pbAt(base.Add(-time.Duration(i+1) * time.Hour))})
	}
	for i := range 2 {
		lpRetroTask(t, env, domain.Task{Project: "beta", Title: fmt.Sprintf("two %d", i), Status: domain.TaskBlocked,
			BlockedCause: "beta two", BlockedReason: "beta two (gave up after 3 attempts)", BlockedAt: pbAt(base.Add(-time.Duration(i+1) * time.Hour))})
	}
	pbLoopError(t, env, loopPRShepherd, "beta", "shepherd broke", base)

	all, err := env.e.Problems(env.ctx)
	require.NoError(t, err)
	var got []string
	for _, p := range all {
		got = append(got, fmt.Sprintf("%s/%s/%s", p.Kind, p.Project, p.Title))
	}
	assert.Equal(t, []string{
		"blocked_goal/alpha/Alpha goal",
		"blocked_tasks/beta/beta five",
		"blocked_tasks/beta/beta two",
		"failing_loops/beta/",
	}, got)
}
