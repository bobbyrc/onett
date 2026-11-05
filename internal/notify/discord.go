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
	"strings"
	"time"
)

const (
	discordTimeout = 10 * time.Second
	// discordErrBodyLimit caps how much of a failing response body goes into the error.
	discordErrBodyLimit = 200
	// Discord's limits on an embed's title and description, in characters.
	discordTitleLimit = 256
	discordDescLimit  = 4096

	discordColorHigh   = 0xE5484D // red
	discordColorNormal = 0x3E63DD // blue
	discordColorLow    = 0x8B8D98 // grey
)

func init() {
	Register("discord", func(cfg Config) (Notifier, error) {
		n, err := newDiscord(cfg.Options, nil)
		if err != nil {
			return nil, err
		}
		return n, nil
	})
}

// discord posts notifications to a Discord channel through a webhook. Outbound only.
type discord struct {
	webhookURL string // embeds a secret token: never log it or put it in an error
	username   string // optional display name override
	client     *http.Client
}

// newDiscord builds a Discord notifier from its YAML options. A nil client uses a default client
// with a 10 second timeout. Options: "webhook_url_env" (required: the name of an environment
// variable holding the webhook URL, which contains a secret token) and "username" (optional).
func newDiscord(opts map[string]any, client *http.Client) (*discord, error) {
	envName := optString(opts, "webhook_url_env")
	if envName == "" {
		return nil, fmt.Errorf("notify discord: option %q is required", "webhook_url_env")
	}
	webhook := strings.TrimSpace(os.Getenv(envName))
	if webhook == "" {
		return nil, fmt.Errorf("notify discord: environment variable %q (webhook_url_env) is empty", envName)
	}
	if client == nil {
		client = &http.Client{Timeout: discordTimeout}
	}
	return &discord{webhookURL: webhook, username: optString(opts, "username"), client: client}, nil
}

type discordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Color       int    `json:"color"`
}

type discordPayload struct {
	Username string         `json:"username,omitempty"`
	Embeds   []discordEmbed `json:"embeds"`
}

// Notify POSTs the notification as one embed. Errors never contain the webhook URL.
func (d *discord) Notify(ctx context.Context, n Notification) error {
	payload := discordPayload{
		Username: d.username,
		Embeds: []discordEmbed{{
			Title:       truncateRunes(n.Title, discordTitleLimit),
			Description: truncateRunes(n.Body, discordDescLimit),
			URL:         n.URL,
			Color:       discordColor(n.Priority),
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notify discord: encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify discord: build request: %w", redactURLError(err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify discord: post to webhook: %w", redactURLError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, discordErrBodyLimit))
		return fmt.Errorf("notify discord: webhook returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// redactURLError strips the request URL that net/http (and url.Parse) put into their errors.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// discordColor maps an onett priority to an embed colour.
func discordColor(p Priority) int {
	switch effectivePriority(p) {
	case PriorityLow:
		return discordColorLow
	case PriorityHigh:
		return discordColorHigh
	default:
		return discordColorNormal
	}
}

// truncateRunes shortens s to at most limit characters, ending in an ellipsis when it cuts.
func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit-1]) + "…"
}
