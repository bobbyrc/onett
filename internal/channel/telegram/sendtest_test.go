package telegram

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"log/slog"
)

func TestSendTestMessagesEveryAllowedUser(t *testing.T) {
	api := newFakeAPI(t)
	b, err := New(Options{Token: testToken, AllowedUserIDs: []int64{42, 7}, StateFile: filepath.Join(t.TempDir(), "s.json"), APIURL: api.srv.URL},
		newFakeHost(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SendTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	var chats []float64
	for _, c := range api.byMethod("sendMessage") {
		chats = append(chats, c.Body["chat_id"].(float64))
		if !strings.Contains(c.Body["text"].(string), "test message") {
			t.Fatalf("text = %v", c.Body["text"])
		}
	}
	if !slices.Equal(chats, []float64{7, 42}) {
		t.Fatalf("chats = %v", chats)
	}
}

func TestSendTestErrorHasNoToken(t *testing.T) {
	api := newFakeAPI(t)
	api.srv.Close()
	b, err := New(Options{Token: testToken, AllowedUserIDs: []int64{42}, StateFile: filepath.Join(t.TempDir(), "s.json"), APIURL: api.srv.URL},
		newFakeHost(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	err = b.SendTest(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error = %v", err)
	}
}
