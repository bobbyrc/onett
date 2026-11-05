package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/notify"
)

// The test routes let the owner check from the config page that a notifier or chat channel
// really delivers. They are owner-only because they send messages with the saved credentials.

// testTimeout bounds one test send.
const testTimeout = 20 * time.Second

// testNotification is what POST /notify/test sends.
var testNotification = notify.Notification{
	Title:    "onett test notification",
	Body:     "onett test notification, sent from the web UI",
	Priority: notify.PriorityNormal,
	Tags:     []string{"test"},
}

// testBody is the request body of both test routes.
type testBody struct {
	Index *int `json:"index"`
}

// testResult is the body of every answer from the test routes.
type testResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func writeTestFailure(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, testResult{Error: msg})
}

// readTestIndex reads {"index": N}.
func (s *server) readTestIndex(w http.ResponseWriter, r *http.Request) (int, bool) {
	var in testBody
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return 0, false
	}
	if in.Index == nil {
		s.fail(w, invalidf("index is required"))
		return 0, false
	}
	return *in.Index, true
}

func (s *server) testNotifier(w http.ResponseWriter, r *http.Request) {
	idx, ok := s.readTestIndex(w, r)
	if !ok {
		return
	}
	cfg := s.opts.Controller.Config()
	if idx < 0 || idx >= len(cfg.Notify) {
		writeErrorMsg(w, http.StatusNotFound, fmt.Sprintf("there is no notifier %d", idx))
		return
	}
	nc := cfg.Notify[idx]
	// Built from the current config by the same factory the fanout uses, so the test exercises
	// what a real notification would.
	n, err := notify.New(notify.Config{Type: nc.Type, Options: nc.Options, MinPriority: notify.Priority(nc.MinPriority)})
	if err != nil {
		s.logTest("notify", idx, nc.Type, nc.Options, err)
		writeTestFailure(w, http.StatusUnprocessableEntity, scrubTestError(err, nc.Options))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), testTimeout)
	defer cancel()
	if err := n.Notify(ctx, testNotification); err != nil {
		s.logTest("notify", idx, nc.Type, nc.Options, err)
		writeTestFailure(w, http.StatusBadGateway, scrubTestError(err, nc.Options))
		return
	}
	s.logTest("notify", idx, nc.Type, nc.Options, nil)
	writeJSON(w, http.StatusOK, testResult{OK: true})
}

func (s *server) testChannel(w http.ResponseWriter, r *http.Request) {
	idx, ok := s.readTestIndex(w, r)
	if !ok {
		return
	}
	cfg := s.opts.Controller.Config()
	if idx < 0 || idx >= len(cfg.Channels) {
		writeErrorMsg(w, http.StatusNotFound, fmt.Sprintf("there is no channel %d", idx))
		return
	}
	ch := cfg.Channels[idx]
	if s.opts.ChannelTest == nil {
		writeTestFailure(w, http.StatusNotImplemented, "this daemon cannot test channels")
		return
	}
	// Only the first channel of each type runs.
	for _, earlier := range cfg.Channels[:idx] {
		if earlier.Type == ch.Type {
			writeTestFailure(w, http.StatusConflict, "this channel is not running: only the first "+ch.Type+" channel is used")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), testTimeout)
	defer cancel()
	err := s.opts.ChannelTest(ctx, ch)
	s.logTest("channel", idx, ch.Type, ch.Options, err)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, testResult{OK: true})
	case errors.Is(err, channel.ErrNotRunning):
		writeTestFailure(w, http.StatusConflict, scrubTestError(err, ch.Options))
	case errors.Is(err, channel.ErrNoTest):
		writeTestFailure(w, http.StatusNotImplemented, scrubTestError(err, ch.Options))
	default:
		writeTestFailure(w, http.StatusBadGateway, scrubTestError(err, ch.Options))
	}
}

// logTest records the outcome of a test send. The error is scrubbed, so no secret value is logged.
func (s *server) logTest(kind string, idx int, typ string, opts map[string]any, err error) {
	if err == nil {
		s.log.Info("api: test send ok", "kind", kind, "index", idx, "type", typ)
		return
	}
	s.log.Warn("api: test send failed", "kind", kind, "index", idx, "type", typ, "err", scrubTestError(err, opts))
}

// scrubTestError is the error text with every secret an entry could hold replaced: the values
// of the environment variables its *_env options name, and any option value other than the
// ones a person needs to see in a failure (the topic and the server).
func scrubTestError(err error, opts map[string]any) string {
	var secrets []string
	for k, v := range opts {
		str := strings.TrimSpace(fmt.Sprint(v))
		if str == "" || v == nil {
			continue
		}
		switch {
		case strings.HasSuffix(k, "_env"):
			if val := os.Getenv(str); val != "" {
				secrets = append(secrets, val)
			}
		case k == "topic" || k == "server" || k == "type":
		default:
			if _, isStr := v.(string); isStr && len(str) >= 6 {
				secrets = append(secrets, str)
			}
		}
	}
	// Longest first, so a value that contains another is replaced whole.
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	msg := err.Error()
	for _, v := range secrets {
		msg = strings.ReplaceAll(msg, v, "[redacted]")
	}
	return msg
}
