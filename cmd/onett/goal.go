package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/bobbyrc/onett/internal/client"
)

// ---- goals ----

func cmdGoal(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: onett goal create|list|show|pause|resume|done|block ...")
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) || sub == "help" {
		fmt.Fprintln(e.out, goalHelp)
		return nil
	}
	switch sub {
	case "create":
		return goalCreate(e, rest)
	case "list":
		return goalList(e, rest)
	case "show":
		return goalShow(e, rest)
	case "pause", "resume":
		return goalMove(e, sub, rest)
	case "done":
		return goalFinish(e, "done", "summary", rest)
	case "block":
		return goalFinish(e, "block", "reason", rest)
	}
	return usagef("unknown goal command %q (want create, list, show, pause, resume, done or block)", sub)
}

func goalCreate(e *env, args []string) error {
	fs := newFlagSet("goal create", e)
	project := fs.String("project", "", "project name (required)")
	title := fs.String("title", "", "one-line title (required)")
	body := fs.String("body", "", "what the goal is and what done looks like")
	bodyFile := fs.String("body-file", "", "read the body from this file ('-' for stdin)")
	jsonOut := fs.Bool("json", false, "print the created goal as JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "goal create --project P --title T [--body B | --body-file F]"); err != nil {
		return err
	}
	if *project == "" || *title == "" {
		return usagef("goal create needs --project and --title")
	}
	if *body != "" && *bodyFile != "" {
		return usagef("use --body or --body-file, not both")
	}
	text := *body
	if *bodyFile != "" {
		data, err := readBodyFile(*bodyFile)
		if err != nil {
			return err
		}
		text = string(data)
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	g, err := c.CreateGoal(context.Background(), client.NewGoal{Project: *project, Title: *title, Body: text})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, g)
	}
	fmt.Fprintln(e.out, g.ID)
	return nil
}

func goalList(e *env, args []string) error {
	fs := newFlagSet("goal list", e)
	project := fs.String("project", "", "only this project")
	status := fs.String("status", "", "comma-separated statuses: active, paused, blocked, done")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "goal list [--project P] [--status a,b]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	goals, err := c.ListGoals(context.Background(), *project, splitList(*status))
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, goals)
	}
	rows := make([][]string, 0, len(goals))
	for _, g := range goals {
		rows = append(rows, []string{g.ID, string(g.Status), g.Project, fmt.Sprint(g.Turns), truncate(g.Title, titleWidth)})
	}
	table(e.out, []string{"ID", "STATUS", "PROJECT", "TURNS", "TITLE"}, rows)
	return nil
}

func goalShow(e *env, args []string) error {
	fs := newFlagSet("goal show", e)
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "goal show ID [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	d, err := c.GetGoal(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, d)
	}
	g := d.Goal
	fmt.Fprintf(e.out, "Goal %s: %s\n", oneLine(g.ID), oneLine(g.Title))
	fmt.Fprintf(e.out, "Status:       %s   Project: %s   Turns: %d\n", oneLine(string(g.Status)), oneLine(g.Project), g.Turns)
	fmt.Fprintf(e.out, "Conversation: %s\n", orDash(oneLine(g.ConversationID)))
	if g.Summary != "" {
		fmt.Fprintf(e.out, "Summary:      %s\n", oneLine(g.Summary))
	}
	if g.Body != "" {
		fmt.Fprintf(e.out, "\n%s\n", strings.TrimSpace(cleanText(g.Body)))
	}
	rows := make([][]string, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		pr := "-"
		if t.PRNumber > 0 {
			pr = fmt.Sprintf("#%d", t.PRNumber)
		}
		rows = append(rows, []string{t.ID, string(t.Status), string(t.Kind), pr, truncate(t.Title, titleWidth)})
	}
	if len(rows) > 0 {
		fmt.Fprintln(e.out)
		table(e.out, []string{"TASK", "STATUS", "KIND", "PR", "TITLE"}, rows)
	}
	return nil
}

// goalMove pauses or resumes a goal.
func goalMove(e *env, verb string, args []string) error {
	positional, err := parseArgs(newFlagSet("goal "+verb, e), args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "goal "+verb+" ID"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	g, err := c.GoalAction(context.Background(), positional[0], verb, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.out, "goal %s is %s\n", g.ID, g.Status)
	return nil
}

// goalFinish ends a goal: done with a summary, or blocked with a reason.
func goalFinish(e *env, verb, flagName string, args []string) error {
	fs := newFlagSet("goal "+verb, e)
	text := fs.String(flagName, "", flagName+" (required)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "goal "+verb+" ID --"+flagName+" TEXT"); err != nil {
		return err
	}
	if strings.TrimSpace(*text) == "" {
		return usagef("goal %s needs --%s", verb, flagName)
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	g, err := c.GoalAction(context.Background(), positional[0], verb, map[string]string{flagName: *text})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.out, "goal %s is %s\n", g.ID, g.Status)
	return nil
}
