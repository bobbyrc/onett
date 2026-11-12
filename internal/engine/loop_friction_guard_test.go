package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/require"
)

// fgSecret is a value shaped like a credential. The engine redacts the values of secret-named
// variables from its own environment, so each test sets one of those.
const fgSecret = "s3cr3t-value-123456"

// TestFrictionTextPostedToGitHubIsRedacted: agent-written friction text goes to a tracker that may
// be public, so the issue and the comment carry the redacted text, while the stored row keeps the
// original.
func TestFrictionTextPostedToGitHubIsRedacted(t *testing.T) {
	t.Setenv("DEMO_DEPLOY_TOKEN", fgSecret)
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))

	env.e.recordFriction(env.ctx, domain.Task{ID: "task-1", Project: "demo"}, domain.Run{ID: "run-1"},
		[]domain.FrictionReport{{Title: "Deploy prints " + fgSecret, Body: "The log showed " + fgSecret + " twice.", Target: "project"}})
	env.run(loopFriction)
	first := fdOnlyIssue(t, env)
	issue := env.fake.Issues[first]
	require.Equal(t, "Deploy prints ***", issue.Title)
	require.NotContains(t, issue.Body, fgSecret)
	require.Contains(t, issue.Body, "The log showed *** twice.")

	env.e.recordFriction(env.ctx, domain.Task{ID: "task-1", Project: "demo"}, domain.Run{ID: "run-2"},
		[]domain.FrictionReport{{
			Title:  "Release notes carry the credential too: " + fgSecret,
			Body:   "Found " + fgSecret + " in the notes.",
			Target: "project",
			Issue:  domain.IssueRef(first),
		}})
	env.run(loopFriction)
	require.Len(t, env.fake.Issues, 1)
	comments := env.fake.Comments[first]
	require.Len(t, comments, 1)
	require.NotContains(t, comments[0], fgSecret)
	require.Contains(t, comments[0], "Found *** in the notes.")

	rows, err := env.st.ListFriction(env.ctx, "demo", 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	stored := false
	for _, r := range rows {
		stored = stored || strings.Contains(r.Body, fgSecret)
	}
	require.True(t, stored, "the stored friction row should keep the original text")
}

// TestIssueReportTextIsRedacted: the summary or blocked reason posted on an intake issue is
// redacted too.
func TestIssueReportTextIsRedacted(t *testing.T) {
	t.Setenv("DEMO_DEPLOY_TOKEN", fgSecret)
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Deploy is broken", "It fails.")
	task := lpIssueTask(t, env, issue)

	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.BlockedReason = domain.TaskBlocked, "The registry rejected "+fgSecret+"; need a new one."
	})

	got := env.fake.Comments[issue.Number]
	require.Len(t, got, 1)
	require.NotContains(t, got[0], fgSecret)
	require.Contains(t, got[0], "The registry rejected ***; need a new one.")
}

// TestIssueReportRedactsBeforeTrimming: a secret that straddles the length cut must not be posted
// in part.
func TestIssueReportRedactsBeforeTrimming(t *testing.T) {
	t.Setenv("DEMO_DEPLOY_TOKEN", fgSecret)
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Deploy is broken", "It fails.")
	task := lpIssueTask(t, env, issue)

	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.BlockedReason = domain.TaskBlocked, strings.Repeat("x", issueReportMax-5)+fgSecret+" tail"
	})

	got := env.fake.Comments[issue.Number]
	require.Len(t, got, 1)
	require.NotContains(t, got[0], fgSecret[:5])
	require.Contains(t, got[0], "***")
}

// TestFrictionOpensAtMostTwentyIssuesADay: the cap stops a misbehaving agent flooding the tracker.
// Reports over it are kept and filed on a later day.
func TestFrictionOpensAtMostTwentyIssuesADay(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	for i := 1; i <= 25; i++ {
		_, _, err := env.st.UpsertFriction(env.ctx, domain.Friction{
			Project: "demo", RunID: "run-x",
			Title:       fmt.Sprintf("alpha%d bravo%d charlie%d", i, i, i),
			Body:        fmt.Sprintf("delta%d echo%d foxtrot%d", i, i, i),
			Fingerprint: fmt.Sprintf("fp-%d", i),
			Count:       1,
		})
		require.NoError(t, err)
	}
	unfiled := func() int {
		rows, err := env.st.ListFriction(env.ctx, "demo", 200)
		require.NoError(t, err)
		n := 0
		for _, r := range rows {
			if r.IssueURL == "" {
				n++
			}
		}
		return n
	}

	env.run(loopFriction)
	require.Len(t, env.fake.Issues, 20)
	require.Equal(t, 5, unfiled())
	rows, err := env.st.ListFriction(env.ctx, "demo", 200)
	require.NoError(t, err)
	require.Len(t, rows, 25)

	env.run(loopFriction)
	require.Len(t, env.fake.Issues, 20)
	require.Equal(t, 5, unfiled())

	// A new report of a problem that already has an issue still attaches on the capped day: it is
	// newer than the reports waiting for a new issue, so the loop reaches it first.
	filedNumber := 0
	for _, r := range rows {
		if r.IssueURL == "" {
			continue
		}
		for n, is := range env.fake.Issues {
			if is.URL == r.IssueURL {
				filedNumber = n
			}
		}
		break
	}
	require.NotZero(t, filedNumber)
	env.e.recordFriction(env.ctx, domain.Task{ID: "task-1", Project: "demo"}, domain.Run{ID: "run-again"},
		[]domain.FrictionReport{{Title: "golf hotel india", Body: "juliett kilo lima", Target: "project", Issue: domain.IssueRef(filedNumber)}})
	env.run(loopFriction)
	require.Len(t, env.fake.Issues, 20)
	require.Len(t, env.fake.Comments[filedNumber], 1)
	require.Equal(t, 5, unfiled())

	// The next day the cap resets.
	env.now = env.now.Add(24 * time.Hour)
	env.run(loopFriction)
	require.Len(t, env.fake.Issues, 25)
	require.Equal(t, 0, unfiled())
}
