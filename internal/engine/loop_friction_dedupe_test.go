package engine

// The fixtures in testdata/friction_issues.json are the real titles and bodies of friction issues
// filed on 2026-11-05 and 2026-11-06. Twelve of them (the "port" ones) report one fault: the fixed
// Playwright preview ports. The tests replay those reports through the friction loop to check that
// reports of one problem share an issue and reports of different problems do not.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
)

// fdReport is one real friction issue from testdata/friction_issues.json: its GitHub number, its
// title and the body the agent wrote.
type fdReport struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// fdPorts are the twelve issues that all report the fixed Playwright preview ports, oldest first.
var fdPorts = []int{224, 227, 230, 231, 233, 235, 238, 241, 249, 253, 259, 264}

// fdLoad reads the fixture file and returns its reports keyed by GitHub number.
func fdLoad(t *testing.T) map[int]fdReport {
	t.Helper()
	raw, err := os.ReadFile("testdata/friction_issues.json")
	if err != nil {
		t.Fatal(err)
	}
	var list []fdReport
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	out := make(map[int]fdReport, len(list))
	for _, r := range list {
		out[r.Number] = r
	}
	if len(out) != 15 {
		t.Fatalf("fixture has %d reports, want 15", len(out))
	}
	return out
}

// fdReportOnce records the report the way a finished run does, then runs the loop once.
func fdReportOnce(t *testing.T, env *lpEnv, r fdReport, issue int) {
	t.Helper()
	env.e.recordFriction(env.ctx, domain.Task{ID: "task-1", Project: "demo"},
		domain.Run{ID: fmt.Sprintf("run-%d", r.Number)},
		[]domain.FrictionReport{{Title: r.Title, Body: r.Body, Target: "project", Issue: domain.IssueRef(issue)}})
	env.run(loopFriction)
}

// fdIssueOf returns the number of the issue that the friction row with this title is linked to,
// or 0 when it is not linked to any issue.
func fdIssueOf(t *testing.T, env *lpEnv, title string) int {
	t.Helper()
	rows, err := env.st.ListFriction(env.ctx, "demo", 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Title != title {
			continue
		}
		if row.IssueURL == "" {
			return 0
		}
		for n, is := range env.fake.Issues {
			if is.URL == row.IssueURL {
				return n
			}
		}
		return 0
	}
	t.Fatalf("no friction row titled %q", title)
	return 0
}

// fdOnlyIssue returns the number of the single issue in the fake SCM.
func fdOnlyIssue(t *testing.T, env *lpEnv) int {
	t.Helper()
	if len(env.fake.Issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(env.fake.Issues))
	}
	for n := range env.fake.Issues {
		return n
	}
	return 0
}

// fdComment is the comment the loop posts when a report joins an existing issue.
func fdComment(r fdReport) string {
	return "Reported again by run `run-" + fmt.Sprint(r.Number) + "`: " + r.Title + "\n\n" + r.Body
}

// TestFrictionPortReportsWithClaimShareOneIssue: when each later report names the first issue as
// the same problem, all twelve reports join that one issue.
func TestFrictionPortReportsWithClaimShareOneIssue(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	reports := fdLoad(t)

	fdReportOnce(t, env, reports[224], 0)
	first := fdOnlyIssue(t, env)
	for _, n := range fdPorts[1:] {
		fdReportOnce(t, env, reports[n], first)
	}

	if n := len(env.fake.Issues); n != 1 {
		t.Fatalf("got %d issues, want 1", n)
	}
	for _, n := range fdPorts {
		if got := fdIssueOf(t, env, reports[n].Title); got != first {
			t.Errorf("report %d linked to issue %d, want %d", n, got, first)
		}
	}
	comments := env.fake.Comments[first]
	if len(comments) != 11 {
		t.Fatalf("got %d comments on the issue, want 11", len(comments))
	}
	for i, c := range comments {
		if want := fdComment(reports[fdPorts[i+1]]); c != want {
			t.Errorf("comment %d = %q, want %q", i, c, want)
		}
	}
}

// TestFrictionPortReportsWithoutClaimOpenTwoIssues pins the word-overlap threshold: the two issues
// describe two symptoms (a preview server silently reused; a run failing because the port is
// taken), and only the agent's claim joins them. Without a claim, the reports split in two.
func TestFrictionPortReportsWithoutClaimOpenTwoIssues(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	reports := fdLoad(t)

	for _, n := range fdPorts {
		fdReportOnce(t, env, reports[n], 0)
	}

	if n := len(env.fake.Issues); n != 2 {
		t.Fatalf("got %d issues, want 2", n)
	}
	a := fdIssueOf(t, env, reports[224].Title)
	b := fdIssueOf(t, env, reports[230].Title)
	if a == 0 || b == 0 || a == b {
		t.Fatalf("issues for 224 and 230 = %d and %d, want two different non-zero issues", a, b)
	}
	if got := env.fake.Issues[a].Title; got != reports[224].Title {
		t.Errorf("issue %d title = %q, want %q", a, got, reports[224].Title)
	}
	if got := env.fake.Issues[b].Title; got != reports[230].Title {
		t.Errorf("issue %d title = %q, want %q", b, got, reports[230].Title)
	}
	for _, n := range []int{227, 235} {
		if got := fdIssueOf(t, env, reports[n].Title); got != a {
			t.Errorf("report %d linked to issue %d, want %d", n, got, a)
		}
	}
	for _, n := range []int{231, 233, 238, 241, 249, 253, 259, 264} {
		if got := fdIssueOf(t, env, reports[n].Title); got != b {
			t.Errorf("report %d linked to issue %d, want %d", n, got, b)
		}
	}
	if n := len(env.fake.Comments[a]); n != 2 {
		t.Errorf("got %d comments on issue %d, want 2", n, a)
	}
	if n := len(env.fake.Comments[b]); n != 8 {
		t.Errorf("got %d comments on issue %d, want 8", n, b)
	}
}

// TestFrictionDifferentProblemsGetTheirOwnIssues: 121 and 179 are two different flaky tests; 320
// shares the words test:mobile and playwright.config.ts with 224 but is another problem.
func TestFrictionDifferentProblemsGetTheirOwnIssues(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	reports := fdLoad(t)

	ids := []int{121, 179, 224, 320}
	for _, n := range ids {
		fdReportOnce(t, env, reports[n], 0)
	}

	if n := len(env.fake.Issues); n != 4 {
		t.Fatalf("got %d issues, want 4", n)
	}
	seen := map[int]bool{}
	for _, n := range ids {
		got := fdIssueOf(t, env, reports[n].Title)
		if got == 0 || seen[got] {
			t.Errorf("report %d linked to issue %d, want a new non-zero issue", n, got)
		}
		seen[got] = true
	}
	for num := range env.fake.Issues {
		if n := len(env.fake.Comments[num]); n != 0 {
			t.Errorf("issue %d got %d comments, want 0", num, n)
		}
	}
}

// TestFrictionRejectedClaimFilesNewIssue: a claim naming an issue that is missing, closed, or not
// filed by onett is ignored, and the report gets its own issue.
func TestFrictionRejectedClaimFilesNewIssue(t *testing.T) {
	t.Run("no such issue", func(t *testing.T) {
		env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
		reports := fdLoad(t)

		fdReportOnce(t, env, reports[224], 0)
		fdReportOnce(t, env, reports[121], 99)

		if n := len(env.fake.Issues); n != 2 {
			t.Fatalf("got %d issues, want 2", n)
		}
		if n := len(env.fake.Comments[fdIssueOf(t, env, reports[224].Title)]); n != 0 {
			t.Errorf("got %d comments on the first issue, want 0", n)
		}
	})

	t.Run("closed issue", func(t *testing.T) {
		env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
		reports := fdLoad(t)

		fdReportOnce(t, env, reports[224], 0)
		first := fdOnlyIssue(t, env)
		if err := env.fake.CloseIssue(env.ctx, first, "fixed"); err != nil {
			t.Fatal(err)
		}
		fdReportOnce(t, env, reports[121], first)

		if n := len(env.fake.Issues); n != 2 {
			t.Fatalf("got %d issues, want 2", n)
		}
		if got := env.fake.Comments[first]; len(got) != 1 || got[0] != "fixed" {
			t.Errorf("comments on closed issue = %q, want only the close comment", got)
		}
	})

	t.Run("issue onett did not file", func(t *testing.T) {
		env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
		reports := fdLoad(t)

		manual := env.fake.AddIssue("Something a person filed", "by hand", "friction")
		fdReportOnce(t, env, reports[121], manual.Number)

		if n := len(env.fake.Issues); n != 2 {
			t.Fatalf("got %d issues, want 2", n)
		}
		if n := len(env.fake.Comments[manual.Number]); n != 0 {
			t.Errorf("got %d comments on the person's issue, want 0", n)
		}
		if got := fdIssueOf(t, env, reports[121].Title); got == 0 || got == manual.Number {
			t.Errorf("report 121 linked to issue %d, want a new issue", got)
		}
	})
}

// TestFrictionRejectedClaimStillMatchesByText: a bad claim does not stop the text match, so 231
// still joins the open issue for 230.
func TestFrictionRejectedClaimStillMatchesByText(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	reports := fdLoad(t)

	fdReportOnce(t, env, reports[230], 0)
	first := fdOnlyIssue(t, env)
	fdReportOnce(t, env, reports[231], 99)

	if n := len(env.fake.Issues); n != 1 {
		t.Fatalf("got %d issues, want 1", n)
	}
	if got := fdIssueOf(t, env, reports[231].Title); got != first {
		t.Errorf("report 231 linked to issue %d, want %d", got, first)
	}
	comments := env.fake.Comments[first]
	if len(comments) != 1 || comments[0] != fdComment(reports[231]) {
		t.Errorf("comments = %q, want exactly the comment for 231", comments)
	}
}

// TestFrictionStopsCommentingAfterTenReports: an issue that already has ten "Reported again"
// comments takes the link but no more comments. Nine such comments plus other talk still allow one.
func TestFrictionStopsCommentingAfterTenReports(t *testing.T) {
	reportAgain := func(body string) scm.IssueComment {
		return scm.IssueComment{Author: "onett", Body: body}
	}

	t.Run("ten already", func(t *testing.T) {
		env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
		reports := fdLoad(t)

		fdReportOnce(t, env, reports[230], 0)
		first := fdOnlyIssue(t, env)
		var prior []scm.IssueComment
		for i := 0; i < 10; i++ {
			prior = append(prior, reportAgain("Reported again by run `old`: earlier\n\nreport"))
		}
		env.fake.Issues[first].Comments = prior

		fdReportOnce(t, env, reports[231], 0)

		if n := len(env.fake.Issues); n != 1 {
			t.Fatalf("got %d issues, want 1", n)
		}
		if got := fdIssueOf(t, env, reports[231].Title); got != first {
			t.Errorf("report 231 linked to issue %d, want %d", got, first)
		}
		if n := len(env.fake.Comments[first]); n != 0 {
			t.Errorf("got %d new comments after ten reports, want 0", n)
		}
	})

	t.Run("nine and people talking", func(t *testing.T) {
		env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
		reports := fdLoad(t)

		fdReportOnce(t, env, reports[230], 0)
		first := fdOnlyIssue(t, env)
		var prior []scm.IssueComment
		for i := 0; i < 9; i++ {
			prior = append(prior, reportAgain("Reported again by run `old`: earlier\n\nreport"))
		}
		for i := 0; i < 3; i++ {
			prior = append(prior, reportAgain("I think this is fixed"))
		}
		env.fake.Issues[first].Comments = prior

		fdReportOnce(t, env, reports[231], 0)

		if n := len(env.fake.Issues); n != 1 {
			t.Fatalf("got %d issues, want 1", n)
		}
		if got := fdIssueOf(t, env, reports[231].Title); got != first {
			t.Errorf("report 231 linked to issue %d, want %d", got, first)
		}
		if n := len(env.fake.Comments[first]); n != 1 {
			t.Errorf("got %d new comments after nine reports, want 1", n)
		}
	})
}

// TestFrictionIssueFieldJSON checks the wire names: the agent's outbox says "issue", and the
// stored record omits it when no issue is named.
func TestFrictionIssueFieldJSON(t *testing.T) {
	var r domain.FrictionReport
	if err := json.Unmarshal([]byte(`{"title":"t","body":"b","target":"onett","issue":224}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Issue != 224 {
		t.Errorf("FrictionReport.Issue = %d, want 224", r.Issue)
	}

	with, err := json.Marshal(domain.Friction{ID: "f", Issue: 224})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"issue":224`) {
		t.Errorf("marshal = %s, want an issue field of 224", with)
	}

	without, err := json.Marshal(domain.Friction{ID: "f"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), `"issue"`) {
		t.Errorf("marshal = %s, want no issue field when 0", without)
	}
}
