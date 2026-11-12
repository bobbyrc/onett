package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

const (
	conversationsUsage     = "conversations [--tasks] [--json] | conversations show ID | conversations show --task TASK_ID"
	conversationsShowUsage = "conversations show ID [--json] | conversations show --task TASK_ID [--json]"
)

// cmdConversations lists the chats with the chief of staff (with --tasks, the task agents'
// conversations instead), or with `show` prints one conversation's messages.
func cmdConversations(e *env, args []string) error {
	if len(args) > 0 && args[0] == "show" {
		return conversationsShow(e, args[1:])
	}
	fs := triggerFlags(e, "conversations", "usage: onett "+conversationsUsage)
	tasks := fs.Bool("tasks", false, "list the task agents' conversations instead of the chats with the chief of staff")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, conversationsUsage); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	if *tasks {
		agents, err := c.AgentConversations(context.Background())
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(e.out, agents)
		}
		if len(agents) == 0 {
			fmt.Fprintln(e.out, "no task agent conversations")
			return nil
		}
		rows := make([][]string, 0, len(agents))
		for _, a := range agents {
			state := "-"
			switch {
			case a.Closed:
				state = "closed"
			case a.Live:
				state = "live"
			}
			rows = append(rows, []string{a.TaskID, a.Project, config.CanonicalAgentName(a.Role), string(a.TaskStatus),
				state, stamp(a.UpdatedAt), truncate(a.Title, titleWidth)})
		}
		table(e.out, []string{"TASK", "PROJECT", "ROLE", "STATUS", "STATE", "UPDATED", "TITLE"}, rows)
		return nil
	}
	convs, err := c.Conversations(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, convs)
	}
	if len(convs) == 0 {
		fmt.Fprintln(e.out, "no conversations")
		return nil
	}
	rows := make([][]string, 0, len(convs))
	for _, cv := range convs {
		rows = append(rows, []string{cv.ID, orDash(cv.Project), conversationState(cv), stamp(cv.UpdatedAt),
			truncate(cv.Title, titleWidth)})
	}
	table(e.out, []string{"ID", "PROJECT", "STATE", "UPDATED", "TITLE"}, rows)
	return nil
}

// conversationState is the STATE of a chat with the chief of staff: closed once closed, busy
// while the chief of staff is replying, otherwise idle.
func conversationState(c domain.Conversation) string {
	switch {
	case c.ClosedAt != nil:
		return "closed"
	case c.Busy:
		return "busy"
	}
	return "idle"
}

func conversationsShow(e *env, args []string) error {
	fs := triggerFlags(e, "conversations show",
		"usage: onett conversations show ID [--json]",
		"       onett conversations show --task TASK_ID [--json]")
	task := fs.String("task", "", "print the conversation of this task's agent")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	// Exactly one of a conversation ID and --task.
	want := 1
	if *task != "" {
		want = 0
	}
	if err := requireArgs(positional, want, conversationsShowUsage); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	if *task != "" {
		tc, err := c.TaskConversation(context.Background(), *task)
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(e.out, tc)
		}
		writeTaskConversation(e.out, tc)
		return nil
	}
	d, err := c.GetConversation(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, d)
	}
	writeConversation(e.out, d)
	return nil
}

func writeConversation(w io.Writer, d client.ConversationDetail) {
	c := d.Conversation
	fmt.Fprintf(w, "Conversation %s: %s\n", oneLine(c.ID), oneLine(c.Title))
	fmt.Fprintf(w, "State:    %s   Project: %s\n", conversationState(c), orDash(oneLine(c.Project)))
	fmt.Fprintf(w, "Created:  %s   Updated: %s\n", stamp(c.CreatedAt), stamp(c.UpdatedAt))
	writeClosed(w, &c)
	writeMessages(w, d.Messages)
}

func writeTaskConversation(w io.Writer, tc domain.TaskConversation) {
	t := tc.Task
	fmt.Fprintf(w, "Task %s: %s\n", oneLine(t.ID), oneLine(t.Title))
	fmt.Fprintf(w, "Status:   %s   Project: %s   Role: %s\n", oneLine(string(t.Status)), oneLine(t.Project),
		orDash(oneLine(config.CanonicalAgentName(t.Role))))
	// Can send: whether `onett task message` would reach the agent now.
	send := "yes"
	if !tc.CanSend {
		send = "no (" + orDash(oneLine(tc.Reason)) + ")"
	}
	fmt.Fprintf(w, "Can send: %s\n", send)
	if tc.LiveRunID != "" {
		fmt.Fprintf(w, "Live run: %s\n", oneLine(tc.LiveRunID))
	}
	writeClosed(w, tc.Conversation)
	writeMessages(w, tc.Messages)
}

// writeClosed prints when, by whom and why a conversation was closed; nothing for an open one.
func writeClosed(w io.Writer, c *domain.Conversation) {
	if c == nil || c.ClosedAt == nil {
		return
	}
	line := fmt.Sprintf("Closed:   %s by %s", stamp(*c.ClosedAt), orDash(oneLine(c.ClosedBy)))
	if c.ClosedReason != "" {
		line += ": " + oneLine(c.ClosedReason)
	}
	fmt.Fprintln(w, line)
}

// writeMessages prints the messages oldest first, as the API returns them: a header line with
// the time and the sender, then the text. The text is indented, so a line an agent wrote can
// never pass for the header of another message. The prompts onett wrote to the agent itself
// (kind "internal") are left out, as in every client; --json has them.
func writeMessages(w io.Writer, msgs []domain.ChatMessage) {
	shown := 0
	for _, m := range msgs {
		if m.Kind == domain.ChatKindInternal {
			continue
		}
		shown++
		who := oneLine(m.Role)
		if m.Author != "" {
			who += " (" + oneLine(m.Author) + ")"
		}
		if m.Queued {
			who += " queued"
		}
		text := strings.TrimRight(cleanText(m.Text), "\n")
		fmt.Fprintf(w, "\n%s  %s\n  %s\n", stamp(m.CreatedAt), who, strings.ReplaceAll(text, "\n", "\n  "))
		for _, a := range m.Attachments {
			fmt.Fprintf(w, "  [attachment: %s]\n", oneLine(a.Name))
		}
	}
	if shown == 0 {
		fmt.Fprintln(w, "\n(no messages)")
	}
}
