package main

import (
	"context"
	"fmt"
	"strconv"
)

const signalsUsage = "signals [--project P] [--limit N] [--json] | signals show ID [--json]"

// cmdSignals lists production signals (problems an observer such as Sentry reported for a
// deployed project), or with `show ID` prints one in full.
func cmdSignals(e *env, args []string) error {
	if len(args) > 0 && args[0] == "show" {
		return signalsShow(e, args[1:])
	}
	fs := triggerFlags(e, "signals", "usage: onett "+signalsUsage)
	project := fs.String("project", "", "only this project")
	limit := fs.Int("limit", 0, "maximum rows (default 200)")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, signalsUsage); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	signals, err := c.ListSignals(context.Background(), *project, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, signals)
	}
	if len(signals) == 0 {
		fmt.Fprintln(e.out, "no signals")
		return nil
	}
	rows := make([][]string, 0, len(signals))
	for _, s := range signals {
		rows = append(rows, []string{s.ID, s.Project, s.Source, s.Level, strconv.FormatInt(s.Count, 10),
			orDash(s.TaskID), stamp(s.LastSeen), truncate(s.Title, titleWidth)})
	}
	table(e.out, []string{"ID", "PROJECT", "SOURCE", "LEVEL", "COUNT", "TASK", "LAST SEEN", "TITLE"}, rows)
	return nil
}

func signalsShow(e *env, args []string) error {
	fs := triggerFlags(e, "signals show", "usage: onett signals show ID [--json]")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "signals show ID [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	// There is no single-signal endpoint: list and pick, as deploys show does.
	// shortcut: only the server's default page (the newest 200) is searched; add GET /signals/{id}
	// when an older one must be read.
	signals, err := c.ListSignals(context.Background(), "", 0)
	if err != nil {
		return err
	}
	for _, s := range signals {
		if s.ID != positional[0] {
			continue
		}
		if *jsonOut {
			return printJSON(e.out, s)
		}
		field := func(name, value string) { fmt.Fprintf(e.out, "%s: %s\n", name, orDash(oneLine(value))) }
		field("id", s.ID)
		field("project", s.Project)
		field("source", s.Source)
		field("level", s.Level)
		field("count", strconv.FormatInt(s.Count, 10))
		field("title", s.Title)
		field("culprit", s.Culprit)
		field("release", s.Release)
		field("url", s.URL)
		field("fingerprint", s.Fingerprint)
		field("task_id", s.TaskID)
		field("first_seen", stamp(s.FirstSeen))
		field("last_seen", stamp(s.LastSeen))
		field("created_at", stamp(s.CreatedAt))
		field("updated_at", stamp(s.UpdatedAt))
		return nil
	}
	return fmt.Errorf("no signal %q among the newest 200", positional[0])
}
