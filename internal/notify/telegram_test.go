package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const telegramTestToken = "123456:SECRET-token"

type telegramRequest struct {
	method string
	path   string
	header http.Header
	body   map[string]any
}

func newTelegramServer(t *testing.T, status int, reply string) (*httptest.Server, *[]telegramRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []telegramRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		got = append(got, telegramRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: body})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func newTestTelegram(t *testing.T, apiURL string, client *http.Client) *telegram {
	t.Helper()
	t.Setenv("TEST_TELEGRAM_TOKEN", telegramTestToken)
	tg, err := newTelegram(map[string]any{"bot_token_env": "TEST_TELEGRAM_TOKEN", "chat_id": "-1001", "api_url": apiURL}, client)
	require.NoError(t, err)
	return tg
}

func TestTelegramRequestShape(t *testing.T) {
	srv, got := newTelegramServer(t, 200, `{"ok":true}`)
	tg := newTestTelegram(t, srv.URL+"/", srv.Client())

	err := tg.Notify(context.Background(), Notification{Title: "Blocked", Body: "task 1", URL: "https://onett.local/t/1", Priority: PriorityHigh})
	require.NoError(t, err)

	require.Len(t, *got, 1)
	r := (*got)[0]
	assert.Equal(t, http.MethodPost, r.method)
	assert.Equal(t, "/bot"+telegramTestToken+"/sendMessage", r.path)
	assert.Equal(t, "application/json", r.header.Get("Content-Type"))
	assert.Equal(t, "-1001", r.body["chat_id"])
	assert.Equal(t, "Blocked\n\ntask 1\n\nhttps://onett.local/t/1", r.body["text"])
	assert.Equal(t, true, r.body["disable_web_page_preview"])
	assert.NotContains(t, r.body, "parse_mode")
	assert.NotContains(t, r.body, "disable_notification")
}

func TestTelegramLowPriorityIsSilent(t *testing.T) {
	srv, got := newTelegramServer(t, 200, `{"ok":true}`)
	tg := newTestTelegram(t, srv.URL, srv.Client())
	require.NoError(t, tg.Notify(context.Background(), Notification{Title: "x", Priority: PriorityLow}))
	assert.Equal(t, true, (*got)[0].body["disable_notification"])
}

func TestTelegramText(t *testing.T) {
	assert.Equal(t, "T", telegramText(Notification{Title: "T"}))
	assert.Equal(t, "T\n\nB", telegramText(Notification{Title: "T", Body: "B"}))
	assert.Equal(t, "T\n\nU", telegramText(Notification{Title: "T", URL: "U"}))

	long := telegramText(Notification{Title: "T", Body: strings.Repeat("é", 5000)})
	assert.Len(t, []rune(long), telegramMaxText)
	assert.True(t, strings.HasPrefix(long, "T\n\né"))
}

func TestTelegramOptionValidation(t *testing.T) {
	t.Setenv("TEST_TELEGRAM_TOKEN", "tok")
	t.Setenv("TEST_TELEGRAM_EMPTY", "")
	tests := []struct {
		name string
		opts map[string]any
		want string
	}{
		{"missing bot_token_env", map[string]any{"chat_id": "1"}, "bot_token_env"},
		{"unset env var", map[string]any{"bot_token_env": "TEST_TELEGRAM_UNSET_XYZ", "chat_id": "1"}, "is empty"},
		{"empty env var", map[string]any{"bot_token_env": "TEST_TELEGRAM_EMPTY", "chat_id": "1"}, "is empty"},
		{"missing chat_id", map[string]any{"bot_token_env": "TEST_TELEGRAM_TOKEN"}, "chat_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTelegram(tt.opts, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	tg, err := newTelegram(map[string]any{"bot_token_env": "TEST_TELEGRAM_TOKEN", "chat_id": 42}, nil)
	require.NoError(t, err)
	assert.Equal(t, "42", tg.chatID)
	assert.Equal(t, telegramDefaultAPI, tg.apiURL)
}

func TestTelegramErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		reply  string
		want   string
	}{
		{"non-2xx with description", 400, `{"ok":false,"description":"Bad Request: chat not found"}`, "chat not found"},
		{"ok false on 200", 200, `{"ok":false,"description":"Forbidden: bot was blocked"}`, "bot was blocked"},
		{"non-JSON body", 502, "upstream down", "upstream down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newTelegramServer(t, tt.status, tt.reply)
			tg := newTestTelegram(t, srv.URL, srv.Client())
			err := tg.Notify(context.Background(), Notification{Title: "x"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), telegramTestToken)
		})
	}
}

func TestTelegramErrorDescriptionIsCapped(t *testing.T) {
	srv, _ := newTelegramServer(t, 400, `{"ok":false,"description":"`+strings.Repeat("a", 1000)+`"}`)
	tg := newTestTelegram(t, srv.URL, srv.Client())
	err := tg.Notify(context.Background(), Notification{Title: "x"})
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 400)
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestTelegramTransportErrorOmitsToken(t *testing.T) {
	tg := newTestTelegram(t, "https://api.telegram.test", &http.Client{Transport: failingTransport{}})
	err := tg.Notify(context.Background(), Notification{Title: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	assert.NotContains(t, err.Error(), telegramTestToken)
	assert.NotContains(t, err.Error(), "SECRET")
}

func TestTelegramInvalidURLErrorOmitsToken(t *testing.T) {
	tg := newTestTelegram(t, "http://bad host", nil)
	err := tg.Notify(context.Background(), Notification{Title: "x"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET")
}

func TestTelegramRegisteredFactory(t *testing.T) {
	t.Setenv("TEST_TELEGRAM_TOKEN", "tok")
	n, err := New(Config{Type: "telegram", Options: map[string]any{"bot_token_env": "TEST_TELEGRAM_TOKEN", "chat_id": "@chan"}})
	require.NoError(t, err)
	assert.IsType(t, &telegram{}, n)
}
