package discord

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

func newTestBot(t *testing.T, f *fakeDiscord, users ...string) *Bot {
	t.Helper()
	b, err := New(Options{Token: testToken, AllowedUserIDs: users, StateFile: statePath(t), APIURL: f.srv.URL + "/api"},
		newFakeHost(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSendTestOpensDMsAndPostsToEveryAllowedUser(t *testing.T) {
	f := newFakeDiscord(t)
	b := newTestBot(t, f, "42", "7")
	if err := b.SendTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	var opened []any
	for _, c := range f.rest("POST", "/users/@me/channels") {
		opened = append(opened, c.Body["recipient_id"])
		if c.Auth != "Bot "+testToken {
			t.Fatalf("auth = %q", c.Auth)
		}
	}
	if !slices.Equal(opened, []any{"42", "7"}) {
		t.Fatalf("opened DMs for %v", opened)
	}
	for _, ch := range []string{"dm-7", "dm-42"} {
		msgs := f.messages(ch)
		if len(msgs) != 1 || !strings.Contains(msgs[0], "test message") {
			t.Fatalf("%s messages = %v", ch, msgs)
		}
	}
	for _, c := range f.rest("POST", "/messages") {
		mentions, _ := c.Body["allowed_mentions"].(map[string]any)
		if parse, ok := mentions["parse"].([]any); !ok || len(parse) != 0 {
			t.Fatalf("allowed_mentions = %v", c.Body["allowed_mentions"])
		}
	}
}

func TestSendTestFailsWhenNoDMOpens(t *testing.T) {
	f := newFakeDiscord(t)
	f.failPath = "/users/@me/channels"
	err := newTestBot(t, f, "42").SendTest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "could not open a direct message") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error = %v", err)
	}
	if got := f.rest("POST", "/messages"); len(got) != 0 {
		t.Fatalf("posted %d messages", len(got))
	}
}

func TestSendTestPostErrorHasNoToken(t *testing.T) {
	f := newFakeDiscord(t)
	f.failPath = "/channels/"
	err := newTestBot(t, f, "42").SendTest(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error = %v", err)
	}
}
