package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// TestKnownFrictionLinesListEachOpenIssueOnce: closed, unfiled and malformed rows are left out, a
// shared issue is listed once under its oldest report's title, and titles are redacted and cut.
func TestKnownFrictionLinesListEachOpenIssueOnce(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	rows := []domain.Friction{
		{Title: "Second sighting", IssueURL: "https://example.invalid/r/issues/7", IssueState: "open", CreatedAt: t0.Add(2 * time.Hour)},
		{Title: "Closed thing", IssueURL: "https://example.invalid/r/issues/5", IssueState: "closed", CreatedAt: t0},
		{Title: "Not filed yet"},
		{Title: "First   report\nof it", IssueURL: "https://example.invalid/r/issues/7", IssueState: "open", CreatedAt: t0},
		{Title: strings.Repeat("x", 150), IssueURL: "https://example.invalid/r/issues/3", IssueState: "open", CreatedAt: t0},
		{Title: "the key is hunter2secret", IssueURL: "https://example.invalid/r/issues/9", IssueState: "open", CreatedAt: t0},
		{Title: "No number", IssueURL: "https://example.invalid/r/issues/", IssueState: "open", CreatedAt: t0},
	}
	got := knownFrictionLines(rows, []string{"API_TOKEN=hunter2secret"})
	want := []string{"- #7: First report of it", "- #3: " + strings.Repeat("x", 100), "- #9: the key is ***"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

// TestKnownFrictionLinesCapAtTwenty: no more than knownFrictionLimit issues are listed.
func TestKnownFrictionLinesCapAtTwenty(t *testing.T) {
	var rows []domain.Friction
	for n := 1; n <= 25; n++ {
		rows = append(rows, domain.Friction{
			Title:      fmt.Sprintf("Problem %d", n),
			IssueURL:   fmt.Sprintf("https://example.invalid/r/issues/%d", n),
			IssueState: "open",
		})
	}
	got := knownFrictionLines(rows, nil)
	if len(got) != 20 {
		t.Fatalf("got %d lines, want 20", len(got))
	}
	if got[0] != "- #1: Problem 1" || got[19] != "- #20: Problem 20" {
		t.Errorf("first and last = %q and %q, want #1 and #20", got[0], got[19])
	}
}

// TestKnownFrictionSectionListsOpenIssues: the section is empty until an issue is open, lists it
// with the claim instructions once one is, and goes away again when the issue is closed.
func TestKnownFrictionSectionListsOpenIssues(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	task := domain.Task{ID: "task-1", Project: "demo"}
	reports := fdLoad(t)

	if got := env.e.knownFrictionSection(env.ctx, task); got != "" {
		t.Fatalf("section with no friction = %q, want empty", got)
	}

	fdReportOnce(t, env, reports[224], 0)
	first := fdOnlyIssue(t, env)
	section := env.e.knownFrictionSection(env.ctx, task)
	for _, want := range []string{
		"## Known friction",
		evidenceNote,
		fmt.Sprintf("- #%d: %s", first, reports[224].Title),
		"`\"issue\": <number>`",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section lacks %q:\n%s", want, section)
		}
	}
	if strings.Contains(section, "For friction with") {
		t.Errorf("single-project section names targets:\n%s", section)
	}

	if err := env.fake.CloseIssue(env.ctx, first, "fixed"); err != nil {
		t.Fatal(err)
	}
	env.run(loopFriction)
	if got := env.e.knownFrictionSection(env.ctx, task); got != "" {
		t.Errorf("section after the issue closed = %q, want empty", got)
	}
}

// TestKnownFrictionSectionGroupsByTarget: with a factory project, the section names each target
// and an agent in the factory sees only its own project's issues.
func TestKnownFrictionSectionGroupsByTarget(t *testing.T) {
	doc := lpDoc(t, lpPath(t),
		"  factory:\n    repo: bobbyrc/factory\n    path: "+filepath.Join(t.TempDir(), "factory")+"\n",
		"self_project: factory\n")
	env := lpNewEngine(t, doc)

	if _, _, err := env.st.UpsertFriction(env.ctx, domain.Friction{Project: "factory", Title: "Factory problem", Fingerprint: "fp-factory", Count: 1, IssueURL: "https://example.invalid/f/issues/4", IssueState: "open"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.st.UpsertFriction(env.ctx, domain.Friction{Project: "demo", Title: "Demo problem", Fingerprint: "fp-demo", Count: 1, IssueURL: "https://example.invalid/d/issues/2", IssueState: "open"}); err != nil {
		t.Fatal(err)
	}

	section := env.e.knownFrictionSection(env.ctx, domain.Task{ID: "task-1", Project: "demo"})
	ordered := []string{
		"For friction with `\"target\": \"project\"`:",
		"- #2: Demo problem",
		"For friction with `\"target\": \"onett\"`:",
		"- #4: Factory problem",
	}
	last := -1
	for _, want := range ordered {
		i := strings.Index(section, want)
		if i < 0 || i < last {
			t.Fatalf("section lacks %q in order:\n%s", want, section)
		}
		last = i
	}

	factory := env.e.knownFrictionSection(env.ctx, domain.Task{ID: "task-2", Project: "factory"})
	if !strings.Contains(factory, "- #4: Factory problem") {
		t.Errorf("factory section lacks its own issue:\n%s", factory)
	}
	if strings.Contains(factory, "For friction with") || strings.Contains(factory, "Demo problem") {
		t.Errorf("factory section leaks other targets:\n%s", factory)
	}
}
