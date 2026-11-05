package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/notify"
)

// fakeNotifier records what it was sent and fails with an error that holds a secret, to prove the
// API scrubs it.
type fakeNotifier struct {
	opts map[string]any
	mu   sync.Mutex
}

var (
	sentMu sync.Mutex
	sent   []notify.Notification
)

func (f *fakeNotifier) Notify(_ context.Context, n notify.Notification) error {
	if f.opts["fail"] == true {
		return fmt.Errorf("post to %v failed", f.opts["webhook_url"])
	}
	sentMu.Lock()
	sent = append(sent, n)
	sentMu.Unlock()
	return nil
}

func init() {
	notify.Register("zz-api-fake", func(cfg notify.Config) (notify.Notifier, error) {
		if cfg.Options["broken"] == true {
			return nil, errors.New("notify zz-api-fake: option \"x\" is required")
		}
		return &fakeNotifier{opts: cfg.Options}, nil
	})
}

func testSendHarness(t *testing.T, chanTest func(context.Context, config.Channel) error) (*harness, string) {
	t.Helper()
	x := newHarness(t)
	x.ctl.cfg = &config.Config{
		Notify: []config.Notifier{
			{Type: "zz-api-fake", Options: map[string]any{}},
			{Type: "zz-api-fake", Options: map[string]any{"fail": true, "webhook_url": "https://hooks.example/SECRETVALUE"}},
			{Type: "zz-api-fake", Options: map[string]any{"broken": true}},
		},
		Channels: []config.Channel{
			{Type: "telegram", Options: map[string]any{"bot_token_env": "ZZ_API_TOK"}},
			{Type: "telegram", Options: map[string]any{}},
		},
	}
	x.h = NewServer(Options{Store: x.st, Bus: x.bus, Controller: x.ctl, Token: testToken, Version: "t", Web: nil, ChannelTest: chanTest})
	return x, x.newUserSession(t, "boss", domain.RoleOwner)
}

func TestNotifyTestSendsLabelledNotification(t *testing.T) {
	sentMu.Lock()
	sent = nil
	sentMu.Unlock()
	x, c := testSendHarness(t, nil)
	rec := x.authReq(t, http.MethodPost, "/api/v1/notify/test", `{"index":0}`, withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
	sentMu.Lock()
	defer sentMu.Unlock()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Body, "onett test notification, sent from the web UI")
}

func TestNotifyTestErrorsNeverHoldASecret(t *testing.T) {
	x, c := testSendHarness(t, nil)
	rec := x.authReq(t, http.MethodPost, "/api/v1/notify/test", `{"index":1}`, withCookie(c))
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	var out testResult
	decode(t, rec, &out)
	assert.False(t, out.OK)
	assert.NotEmpty(t, out.Error)
	assert.NotContains(t, rec.Body.String(), "SECRETVALUE")

	rec = x.authReq(t, http.MethodPost, "/api/v1/notify/test", `{"index":2}`, withCookie(c))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	decode(t, rec, &out)
	assert.Contains(t, out.Error, "is required")
}

func TestNotifyTestBadIndexAndAccess(t *testing.T) {
	x, c := testSendHarness(t, nil)
	assert.Equal(t, http.StatusNotFound, x.authReq(t, http.MethodPost, "/api/v1/notify/test", `{"index":9}`, withCookie(c)).Code)
	assert.Equal(t, http.StatusNotFound, x.authReq(t, http.MethodPost, "/api/v1/notify/test", `{"index":-1}`, withCookie(c)).Code)
	assert.Equal(t, http.StatusBadRequest, x.authReq(t, http.MethodPost, "/api/v1/notify/test", `{}`, withCookie(c)).Code)
	// The API token, which agents hold, cannot send tests.
	assert.Equal(t, http.StatusForbidden, x.authReq(t, http.MethodPost, "/api/v1/notify/test", `{"index":0}`, withToken(testToken)).Code)
	assert.Equal(t, http.StatusForbidden, x.authReq(t, http.MethodPost, "/api/v1/channels/test", `{"index":0}`, withToken(testToken)).Code)
}

func TestChannelTestStatuses(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"ok", nil, http.StatusOK},
		{"not running", fmt.Errorf("%w: environment variable \"ZZ_API_TOK\" is empty", channel.ErrNotRunning), http.StatusConflict},
		{"no tester", channel.ErrNoTest, http.StatusNotImplemented},
		{"send failed", errors.New("telegram sendMessage: HTTP 403: Forbidden"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got config.Channel
			x, c := testSendHarness(t, func(_ context.Context, ch config.Channel) error { got = ch; return tc.err })
			rec := x.authReq(t, http.MethodPost, "/api/v1/channels/test", `{"index":0}`, withCookie(c))
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.Equal(t, "telegram", got.Type)
			var out testResult
			decode(t, rec, &out)
			assert.Equal(t, tc.err == nil, out.OK)
			if tc.err != nil {
				assert.Contains(t, out.Error, tc.err.Error())
			}
		})
	}
}

func TestChannelTestEdgeCases(t *testing.T) {
	called := false
	x, c := testSendHarness(t, func(context.Context, config.Channel) error { called = true; return nil })
	assert.Equal(t, http.StatusNotFound, x.authReq(t, http.MethodPost, "/api/v1/channels/test", `{"index":5}`, withCookie(c)).Code)
	// The second telegram channel is never started: only the first of a type runs.
	rec := x.authReq(t, http.MethodPost, "/api/v1/channels/test", `{"index":1}`, withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.False(t, called)

	x, c = testSendHarness(t, nil)
	assert.Equal(t, http.StatusNotImplemented, x.authReq(t, http.MethodPost, "/api/v1/channels/test", `{"index":0}`, withCookie(c)).Code)
}
