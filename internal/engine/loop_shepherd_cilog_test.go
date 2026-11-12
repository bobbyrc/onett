package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/scm/github"
)

// clgGH stands in for the gh binary: it answers from canned output keyed by the first two
// arguments ("pr view", "run list", "run view") and records every call.
type clgGH struct {
	out   map[string]string
	err   map[string]error
	calls [][]string
}

func (g *clgGH) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	g.calls = append(g.calls, args)
	key := strings.Join(args[:min(2, len(args))], " ")
	if err := g.err[key]; err != nil {
		return nil, err
	}
	return []byte(g.out[key]), nil
}

// clgSCM is the fake code host with CILog served by the real GitHub implementation over canned gh
// output, so these tests cover the path from gh's output to the note the agent reads.
type clgSCM struct {
	scm.SCM
	gh *github.GitHub
}

func (s clgSCM) CILog(ctx context.Context, number int, maxBytes int) (string, error) {
	return s.gh.CILog(ctx, number, maxBytes)
}

// clgEnv is a shepherd environment whose PR has a failed "lint" check, with CILog read through gh.
func clgEnv(t *testing.T) (*lpEnv, domain.Task, *clgGH) {
	t.Helper()
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CIFailure, "lint")
	gh := &clgGH{out: map[string]string{}, err: map[string]error{}}
	gh.out["pr view"] = fmt.Sprintf(`{"number":%d,"state":"OPEN","headRefName":%q,"headRefOid":%q,"statusCheckRollup":[{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE"},{"__typename":"CheckRun","name":"test","status":"IN_PROGRESS","conclusion":""}]}`, task.PRNumber, lpBranch, env.head)
	s := clgSCM{SCM: env.fake, gh: github.NewWithRunner("owner/name", gh.run)}
	env.e.d.SCMFor = func(*config.Project) (scm.SCM, error) { return s, nil }
	return env, task, gh
}

// A fast job fails while a slow job of the same run is still going. The shepherd must not re-queue
// the task with an empty note; once the run ends, the note holds the failed job's log from the
// head commit, redacted and labelled as evidence.
func TestLoopShepherdWaitsForRunningCIThenRequeuesWithFailedJobLog(t *testing.T) {
	t.Setenv("CLG_API_TOKEN", "hunter2hunter2")
	env, task, gh := clgEnv(t)

	// Stage 1: the run is still going.
	gh.out["run list"] = `[{"databaseId":101,"conclusion":"","name":"CI"}]`
	gh.err["run view"] = errors.New("run 101 is still in progress; logs will be available when it is complete")
	env.run(loopPRShepherd)
	got := env.task(task.ID)
	if got.Status != domain.TaskReview {
		t.Fatalf("status = %s, want review while a CI run is still going; body: %q", got.Status, got.Body)
	}
	if strings.Contains(got.Body, "## Attempt") {
		t.Fatalf("note written before the log could be read: %q", got.Body)
	}

	// Stage 2: the run has ended.
	gh.out["run list"] = `[{"databaseId":101,"conclusion":"failure","name":"CI"}]`
	delete(gh.err, "run view")
	gh.out["run view"] = "lint\tRun gofmt\t2026-11-12T16:58:45.7146174Z internal/api/routes.go needs gofmt\nlint\tRun gofmt\t2026-11-12T16:58:45.7194027Z token hunter2hunter2 used\nlint\tRun gofmt\t2026-11-12T16:58:46.0000000Z ##[error]Process completed with exit code 1.\n"
	env.run(loopPRShepherd)
	got = env.task(task.ID)
	if got.Status != domain.TaskQueued {
		t.Fatalf("status = %s, want queued once the run has ended", got.Status)
	}
	for _, want := range []string{
		"## Attempt 0: CI failed: lint",
		evidenceNote,
		"=== CI @ " + env.head[:7] + " ===",
		"=== lint / Run gofmt ===",
		"internal/api/routes.go needs gofmt",
		"token *** used",
	} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("note lacks %q: %q", want, got.Body)
		}
	}
	if strings.Contains(got.Body, "hunter2hunter2") {
		t.Fatalf("secret leaked into the note: %q", got.Body)
	}
	if !slices.ContainsFunc(gh.calls, func(c []string) bool {
		return len(c) > 1 && c[0] == "run" && c[1] == "list" && slices.Contains(c, "--commit") && slices.Contains(c, env.head)
	}) {
		t.Fatalf("no gh run list for the head commit %s: %v", env.head, gh.calls)
	}
}

// A failed run whose failed-step log comes back empty still gets a sentence saying so, under the
// evidence label, rather than a blank block.
func TestLoopShepherdExplainsEmptyCILogFromGH(t *testing.T) {
	env, task, gh := clgEnv(t)
	gh.out["run list"] = `[{"databaseId":101,"conclusion":"failure","name":"CI"}]`
	gh.out["run view"] = "\n"

	env.run(loopPRShepherd)
	got := env.task(task.ID)
	if got.Status != domain.TaskQueued {
		t.Fatalf("status = %s, want queued", got.Status)
	}
	for _, want := range []string{evidenceNote, "=== CI @ " + env.head[:7] + " ===", "no failed-step log"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("note lacks %q: %q", want, got.Body)
		}
	}
}
