package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type discordRequest struct {
	method string
	header http.Header
	body   string
}

func newDiscordServer(t *testing.T, status int, reply string) (*httptest.Server, *[]discordRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []discordRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, discordRequest{method: r.Method, header: r.Header.Clone(), body: string(b)})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func discordFor(t *testing.T, srv *httptest.Server, extra map[string]any) *discord {
	t.Helper()
	t.Setenv("TEST_DISCORD_WEBHOOK", srv.URL+"/api/webhooks/123/secret-token")
	opts := map[string]any{"webhook_url_env": "TEST_DISCORD_WEBHOOK"}
	for k, v := range extra {
		opts[k] = v
	}
	d, err := newDiscord(opts, srv.Client())
	require.NoError(t, err)
	return d
}

func decodeDiscord(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	return m
}

func TestDiscordNotifyPayload(t *testing.T) {
	tests := []struct {
		name      string
		priority  Priority
		wantColor float64
	}{
		{"high", PriorityHigh, 0xE5484D},
		{"normal", PriorityNormal, 0x3E63DD},
		{"empty is normal", "", 0x3E63DD},
		{"low", PriorityLow, 0x8B8D98},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, got := newDiscordServer(t, http.StatusNoContent, "")
			d := discordFor(t, srv, map[string]any{"username": "onett"})
			err := d.Notify(context.Background(), Notification{Title: "Blocked", Body: "task 1 needs you", Priority: tc.priority, URL: "https://onett.local/t/1"})
			require.NoError(t, err)

			require.Len(t, *got, 1)
			r := (*got)[0]
			assert.Equal(t, http.MethodPost, r.method)
			assert.Equal(t, "application/json", r.header.Get("Content-Type"))
			m := decodeDiscord(t, r.body)
			assert.Equal(t, "onett", m["username"])
			embeds := m["embeds"].([]any)
			require.Len(t, embeds, 1)
			e := embeds[0].(map[string]any)
			assert.Equal(t, "Blocked", e["title"])
			assert.Equal(t, "task 1 needs you", e["description"])
			assert.Equal(t, "https://onett.local/t/1", e["url"])
			assert.Equal(t, tc.wantColor, e["color"])
		})
	}
}

func TestDiscordOmitsOptionalFields(t *testing.T) {
	srv, got := newDiscordServer(t, http.StatusNoContent, "")
	d := discordFor(t, srv, nil)
	require.NoError(t, d.Notify(context.Background(), Notification{Title: "Hi"}))
	m := decodeDiscord(t, (*got)[0].body)
	assert.NotContains(t, m, "username")
	e := m["embeds"].([]any)[0].(map[string]any)
	assert.NotContains(t, e, "url")
	assert.NotContains(t, e, "description")
}

func TestDiscordTruncates(t *testing.T) {
	srv, got := newDiscordServer(t, http.StatusNoContent, "")
	d := discordFor(t, srv, nil)
	require.NoError(t, d.Notify(context.Background(), Notification{
		Title: strings.Repeat("é", 300),
		Body:  strings.Repeat("b", 5000),
	}))
	e := decodeDiscord(t, (*got)[0].body)["embeds"].([]any)[0].(map[string]any)
	assert.Len(t, []rune(e["title"].(string)), 256)
	assert.Len(t, []rune(e["description"].(string)), 4096)
	assert.True(t, strings.HasSuffix(e["title"].(string), "…"))
}

func TestDiscordConstructionErrors(t *testing.T) {
	_, err := newDiscord(map[string]any{}, nil)
	require.ErrorContains(t, err, "webhook_url_env")

	t.Setenv("TEST_DISCORD_EMPTY", "")
	_, err = newDiscord(map[string]any{"webhook_url_env": "TEST_DISCORD_EMPTY"}, nil)
	require.ErrorContains(t, err, "TEST_DISCORD_EMPTY")

	_, err = New(Config{Type: "discord", Options: map[string]any{"webhook_url_env": "TEST_DISCORD_EMPTY"}})
	require.Error(t, err)
}

func TestDiscordNon2xxError(t *testing.T) {
	srv, _ := newDiscordServer(t, http.StatusTooManyRequests, strings.Repeat("x", 1000))
	d := discordFor(t, srv, nil)
	err := d.Notify(context.Background(), Notification{Title: "t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Less(t, len(err.Error()), 400)
	assert.NotContains(t, err.Error(), "secret-token")
}

func TestDiscordTransportErrorDoesNotLeakURL(t *testing.T) {
	srv, _ := newDiscordServer(t, http.StatusNoContent, "")
	d := discordFor(t, srv, nil)
	srv.Close() // connection refused; net/http would normally quote the full URL

	err := d.Notify(context.Background(), Notification{Title: "t"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-token")
	assert.NotContains(t, err.Error(), "/api/webhooks")
	assert.Contains(t, err.Error(), "notify discord: post to webhook")
}
