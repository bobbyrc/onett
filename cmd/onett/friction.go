package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bobbyrc/onett/internal/domain"
)

const frictionUsage = "friction [--project P] [--limit N] [--json] | friction show ID [--json]"

// cmdFriction lists friction (what slowed an agent down, as agents reported it), or with
// `show ID` prints one report and its body.
func cmdFriction(e *env, args []string) error {
	if len(args) > 0 && args[0] == "show" {
		return frictionShow(e, args[1:])
	}
	fs := triggerFlags(e, "friction", "usage: onett "+frictionUsage)
	project := fs.String("project", "", "only this project")
	limit := fs.Int("limit", 0, "maximum rows (default 200)")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, frictionUsage); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	reports, err := c.ListFriction(context.Background(), *project, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, reports)
	}
	if len(reports) == 0 {
		fmt.Fprintln(e.out, "no friction")
		return nil
	}
	rows := make([][]string, 0, len(reports))
	for _, f := range reports {
		rows = append(rows, []string{f.ID, f.Project, strconv.Itoa(f.Count), frictionIssue(f),
			stamp(f.UpdatedAt), truncate(f.Title, titleWidth)})
	}
	table(e.out, []string{"ID", "PROJECT", "COUNT", "ISSUE", "UPDATED", "TITLE"}, rows)
	return nil
}

// frictionIssue is the ISSUE cell: the filed issue's URL and, when known, its state.
func frictionIssue(f domain.Friction) string {
	switch {
	case f.IssueURL == "":
		return "-"
	case f.IssueState == "":
		return f.IssueURL
	}
	return f.IssueURL + " (" + f.IssueState + ")"
}

func frictionShow(e *env, args []string) error {
	fs := triggerFlags(e, "friction show", "usage: onett friction show ID [--json]")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "friction show ID [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	// There is no single-friction endpoint: list and pick, as deploys show does.
	// shortcut: only the server's default page (the newest 200) is searched; add GET /friction/{id}
	// when an older one must be read.
	reports, err := c.ListFriction(context.Background(), "", 0)
	if err != nil {
		return err
	}
	for _, f := range reports {
		if f.ID != positional[0] {
			continue
		}
		if *jsonOut {
			return printJSON(e.out, f)
		}
		field := func(name, value string) { fmt.Fprintf(e.out, "%s: %s\n", name, orDash(oneLine(value))) }
		field("id", f.ID)
		field("project", f.Project)
		field("run_id", f.RunID)
		field("title", f.Title)
		field("count", strconv.Itoa(f.Count))
		field("issue_url", f.IssueURL)
		field("issue_state", f.IssueState)
		field("fingerprint", f.Fingerprint)
		field("created_at", stamp(f.CreatedAt))
		field("updated_at", stamp(f.UpdatedAt))
		body := strings.TrimRight(cleanText(f.Body), "\n")
		if body == "" {
			body = "(no body)"
		}
		fmt.Fprintf(e.out, "\n%s\n", body)
		return nil
	}
	return fmt.Errorf("no friction %q", positional[0])
}
