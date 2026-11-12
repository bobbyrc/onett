package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/bobbyrc/onett/internal/domain"
)

// ---- triggers (read-only: the API token may not create, change or fire one) ----

func cmdTrigger(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: onett trigger list|history ...")
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) || sub == "help" {
		fmt.Fprintln(e.out, triggerHelp)
		return nil
	}
	switch sub {
	case "list":
		return triggerList(e, rest)
	case "history":
		return triggerHistory(e, rest)
	}
	return usagef("unknown trigger command %q (want list or history)", sub)
}

// triggerFlags is a flag set whose -h prints the verb's usage line and its flags to stdout.
func triggerFlags(e *env, name string, usageLines ...string) *flag.FlagSet {
	fs := newFlagSet(name, e)
	fs.Usage = func() {
		for _, line := range usageLines {
			fmt.Fprintln(e.out, line)
		}
		fs.SetOutput(e.out)
		fs.PrintDefaults()
	}
	return fs
}

func triggerList(e *env, args []string) error {
	fs := triggerFlags(e, "trigger list",
		"usage: onett trigger list [--json]",
		"Only a signed-in owner creates, changes or runs triggers (in the UI or through the API); the CLI only reads them.")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "trigger list [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	triggers, err := c.Triggers(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, triggers)
	}
	if len(triggers) == 0 {
		fmt.Fprintln(e.out, "no triggers")
		return nil
	}
	rows := make([][]string, 0, len(triggers))
	for _, v := range triggers {
		enabled := "no"
		if v.Enabled {
			enabled = "yes"
		}
		nextRun := "-"
		if v.NextRunAt != nil {
			nextRun = stamp(*v.NextRunAt)
		}
		lastFiring := "-"
		if v.LastFiring != nil {
			lastFiring = oneLine(v.LastFiring.Outcome) + " " + stamp(v.LastFiring.At)
		}
		rows = append(rows, []string{v.ID, truncate(v.Name, titleWidth), string(v.Kind), orDash(oneLine(v.Project)),
			enabled, triggerWhen(v.Trigger), nextRun, lastFiring})
	}
	table(e.out, []string{"ID", "NAME", "KIND", "PROJECT", "ENABLED", "WHEN", "NEXT RUN", "LAST FIRING"}, rows)
	return nil
}

// triggerWhen says when a trigger fires, in words a person reads from the list.
func triggerWhen(t domain.Trigger) string {
	switch t.Kind {
	case domain.TriggerSchedule:
		when := oneLine(t.Cron)
		if t.TimeZone != "" {
			when += " (" + oneLine(t.TimeZone) + ")"
		}
		return when
	case domain.TriggerGitHubIssue:
		if t.Label == "" {
			return "new issue"
		}
		return "new issue labelled " + oneLine(t.Label)
	}
	return "-"
}

func triggerHistory(e *env, args []string) error {
	fs := triggerFlags(e, "trigger history",
		"usage: onett trigger history ID [--limit N]")
	limit := fs.Int("limit", 20, "how many firings to show, newest first")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "trigger history ID [--limit N]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	firings, err := c.TriggerFirings(context.Background(), positional[0], *limit)
	if err != nil {
		return err
	}
	if len(firings) == 0 {
		fmt.Fprintln(e.out, "no firings yet")
		return nil
	}
	rows := make([][]string, 0, len(firings))
	for _, f := range firings {
		rows = append(rows, []string{stamp(f.At), firingCause(f), f.Outcome, orDash(f.Reason), orDash(f.ConversationID)})
	}
	table(e.out, []string{"AT", "CAUSE", "OUTCOME", "REASON", "CONVERSATION"}, rows)
	return nil
}

// firingCause says what made a firing happen, for the CAUSE column of trigger history.
func firingCause(f domain.TriggerFiring) string {
	switch f.Cause {
	case domain.FiringCauseSchedule:
		if f.ScheduledFor != nil {
			return "schedule for " + stamp(*f.ScheduledFor)
		}
		return "schedule"
	case domain.FiringCauseGitHubIssue:
		return fmt.Sprintf("issue #%d", f.IssueNumber)
	case domain.FiringCauseManual:
		return "manual by " + orDash(oneLine(f.By))
	}
	return oneLine(f.Cause)
}
