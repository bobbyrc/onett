package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	telegramDefaultAPI = "https://api.telegram.org"
	telegramTimeout    = 10 * time.Second
	// telegramMaxText is Telegram's message length limit, counted in characters.
	telegramMaxText = 4096
	// telegramErrBodyLimit caps how much of a failing response body goes into the error.
	telegramErrBodyLimit = 200
)

func init() {
	Register("telegram", func(cfg Config) (Notifier, error) {
		n, err := newTelegram(cfg.Options, nil)
		if err != nil {
			return nil, err
		}
		return n, nil
	})
}

// telegram sends notifications to a chat through the Telegram Bot API (outbound only).
type telegram struct {
	apiURL string
	token  string // secret: it is part of the request URL, so it must never reach an error
	chatID string
	client *http.Client
	// skipApprovals drops notifications tagged TagApproval (the two-way Telegram channel owns them).
	skipApprovals bool
}

// newTelegram builds a Telegram notifier from its YAML options. A nil client uses a default
// client with a 10 second timeout. Options: "bot_token_env" (required; the name of an
// environment variable holding the bot token), "chat_id" (required; a numeric ID or
// @channelname), "api_url" (default https://api.telegram.org) and "skip_approvals" (bool; drops
// notifications tagged TagApproval, for use beside the two-way Telegram channel).
func newTelegram(opts map[string]any, client *http.Client) (*telegram, error) {
	envName := optString(opts, "bot_token_env")
	if envName == "" {
		return nil, fmt.Errorf("notify telegram: option %q is required", "bot_token_env")
	}
	token := strings.TrimSpace(os.Getenv(envName))
	if token == "" {
		return nil, fmt.Errorf("notify telegram: environment variable %q (bot_token_env) is empty", envName)
	}
	chatID := optString(opts, "chat_id")
	if chatID == "" {
		return nil, fmt.Errorf("notify telegram: option %q is required", "chat_id")
	}
	apiURL := optString(opts, "api_url")
	if apiURL == "" {
		apiURL = telegramDefaultAPI
	}
	if client == nil {
		client = &http.Client{Timeout: telegramTimeout}
	}
	return &telegram{
		apiURL:        strings.TrimRight(apiURL, "/"),
		token:         token,
		chatID:        chatID,
		client:        client,
		skipApprovals: optBool(opts, "skip_approvals"),
	}, nil
}

// Notify sends the notification as one plain-text message. Low priority is delivered silently.
func (t *telegram) Notify(ctx context.Context, n Notification) error {
	if t.skipApprovals && slices.Contains(n.Tags, TagApproval) {
		return nil
	}
	return t.sendMessage(ctx, telegramText(n), effectivePriority(n.Priority) == PriorityLow)
}

// telegramText is the title, then the body and the URL, each after a blank line, cut to
// Telegram's limit.
func telegramText(n Notification) string {
	parts := []string{n.Title}
	if n.Body != "" {
		parts = append(parts, n.Body)
	}
	if n.URL != "" {
		parts = append(parts, n.URL)
	}
	text := strings.Join(parts, "\n\n")
	if r := []rune(text); len(r) > telegramMaxText {
		text = string(r[:telegramMaxText])
	}
	return text
}

// sendMessage calls the Bot API's sendMessage. No parse_mode is set, so the text is plain and
// needs no escaping. The token is in the URL path and Go's client errors quote the URL, so
// errors here are built from the unwrapped cause and never mention the endpoint.
func (t *telegram) sendMessage(ctx context.Context, text string, silent bool) error {
	payload := map[string]any{
		"chat_id":                  t.chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	if silent {
		payload["disable_notification"] = true
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notify telegram: encode message: %w", err)
	}

	endpoint := t.apiURL + "/bot" + t.token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify telegram: build request: %w", t.scrub(unwrapURLError(err)))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify telegram: send: %w", t.scrub(unwrapURLError(err)))
	}
	defer resp.Body.Close()

	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var reply struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	parsed := json.Unmarshal(snippet, &reply) == nil

	if resp.StatusCode < 200 || resp.StatusCode > 299 || (parsed && !reply.OK) {
		desc := reply.Description
		if !parsed || desc == "" {
			desc = strings.TrimSpace(string(snippet))
		}
		return fmt.Errorf("notify telegram: %s: %s", resp.Status, t.capped(desc))
	}
	return nil
}

// optBool reads a boolean option; a missing key is false.
func optBool(opts map[string]any, key string) bool {
	b, _ := opts[key].(bool)
	return b
}

// unwrapURLError drops the *url.Error wrapper, whose text includes the full request URL.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// scrub is a last line of defence: it replaces the token if it still appears in an error.
func (t *telegram) scrub(err error) error {
	if strings.Contains(err.Error(), t.token) {
		return errors.New(strings.ReplaceAll(err.Error(), t.token, "<redacted>"))
	}
	return err
}

// capped trims s to telegramErrBodyLimit runes and removes the token if it was echoed back.
func (t *telegram) capped(s string) string {
	s = strings.ReplaceAll(s, t.token, "<redacted>")
	if r := []rune(s); len(r) > telegramErrBodyLimit {
		s = string(r[:telegramErrBodyLimit])
	}
	return s
}

var _ Notifier = (*telegram)(nil)
