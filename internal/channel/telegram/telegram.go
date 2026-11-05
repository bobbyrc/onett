// Package telegram is a two-way chat channel between the owner and onett over a Telegram bot.
//
// The bot long-polls getUpdates, so no public URL is needed. Only the Telegram user IDs in the
// allowlist are obeyed, and only in private chats: an allowed user can approve merges, so this is
// the security boundary and everything else is dropped. Each private chat maps to one
// orchestrator conversation. Replies and approval requests arrive from the daemon's event stream,
// which is read in its own goroutine so a slow Telegram call can never stall the daemon.
//
// The bot owns one small JSON state file (poll offset and chat-to-conversation map) so a restart
// does not replay handled updates or lose conversations. The bot token is in every request URL,
// so errors are stripped of the URL before they are returned or logged, and the state file never
// holds it.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/domain"
)

const (
	defaultAPI = "https://api.telegram.org"
	// maxText is Telegram's message length limit, counted in characters.
	maxText = 4096
	// pollTimeout is the getUpdates long-poll length in seconds.
	pollTimeout = 30
	// seenLimit bounds the set of chat message IDs already relayed.
	seenLimit = 500
	// eventBuffer is how many events may queue between the reader and the sender.
	eventBuffer = 256
	maxBackoff  = 60 * time.Second
)

// backoffBase is the first retry delay after a polling error; tests shorten it.
var backoffBase = time.Second

// Host is what the bot needs from the daemon. The daemon passes an adapter over the engine and
// event bus.
type Host interface {
	CreateConversation(ctx context.Context, title string) (domain.Conversation, error)
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)
	Subscribe() (<-chan domain.Event, func())
}

// Options configures a Bot.
type Options struct {
	Token          string       // bot token (never logged)
	AllowedUserIDs []int64      // Telegram user IDs allowed to talk to onett; must be non-empty
	StateFile      string       // JSON file the bot owns, e.g. <data_dir>/channels/telegram.json
	APIURL         string       // default "https://api.telegram.org"
	HTTPClient     *http.Client // nil = default with a timeout above the long-poll length
}

// Bot is the Telegram channel. Create it with New and run it with Run.
type Bot struct {
	token   string
	api     string
	allowed map[int64]bool
	file    string
	client  *http.Client
	host    Host
	log     *slog.Logger

	mu        sync.Mutex
	st        state
	approvals map[string][]sentMsg // approval ID -> messages carrying its keyboard
	seen      map[string]bool
	seenOrder []string
}

// state is what survives a restart.
type state struct {
	Offset int64            `json:"offset"`
	Chats  map[int64]string `json:"chats"` // chat ID -> conversation ID
}

type sentMsg struct {
	ChatID    int64
	MessageID int64
	Text      string
}

// New validates the options and loads the state file.
func New(opts Options, host Host, logger *slog.Logger) (*Bot, error) {
	if opts.Token == "" {
		return nil, errors.New("telegram: token is required")
	}
	if len(opts.AllowedUserIDs) == 0 {
		return nil, errors.New("telegram: allowed_user_ids must not be empty")
	}
	if opts.StateFile == "" {
		return nil, errors.New("telegram: state file is required")
	}
	if host == nil {
		return nil, errors.New("telegram: host is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bot{
		token: opts.Token, api: strings.TrimRight(opts.APIURL, "/"), file: opts.StateFile,
		client: opts.HTTPClient, host: host, log: logger,
		allowed:   map[int64]bool{},
		approvals: map[string][]sentMsg{},
		seen:      map[string]bool{},
	}
	if b.api == "" {
		b.api = defaultAPI
	}
	if b.client == nil {
		b.client = &http.Client{Timeout: (pollTimeout + 15) * time.Second}
	}
	for _, id := range opts.AllowedUserIDs {
		b.allowed[id] = true
	}
	if err := b.load(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Bot) load() error {
	b.st = state{Chats: map[int64]string{}}
	data, err := os.ReadFile(b.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("telegram: read state: %w", err)
	}
	if err := json.Unmarshal(data, &b.st); err != nil {
		return fmt.Errorf("telegram: parse state file: %w", err)
	}
	if b.st.Chats == nil {
		b.st.Chats = map[int64]string{}
	}
	return nil
}

// save writes the state atomically. The caller holds b.mu.
func (b *Bot) save() {
	if err := b.writeState(); err != nil {
		b.log.Warn("telegram: save state failed", "err", err)
	}
}

func (b *Bot) writeState() error {
	data, err := json.Marshal(b.st)
	if err != nil {
		return err
	}
	dir := filepath.Dir(b.file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".telegram-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), b.file)
}

// Run polls Telegram and relays events until ctx is done. It returns nil on cancel.
func (b *Bot) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	events, unsub := b.host.Subscribe()
	defer unsub()

	// The reader never waits on Telegram: it hands events to the sender through a buffer and
	// drops them (with a warning) if the buffer is full.
	queue := make(chan domain.Event, eventBuffer)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				select {
				case queue <- ev:
				default:
					b.log.Warn("telegram: event queue full, dropping event", "type", ev.Type)
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-queue:
				b.handleEvent(ctx, ev)
			}
		}
	}()

	b.poll(ctx)
	cancel()
	wg.Wait()
	return nil
}

func (b *Bot) poll(ctx context.Context) {
	delay := time.Duration(0)
	for ctx.Err() == nil {
		b.mu.Lock()
		offset := b.st.Offset
		b.mu.Unlock()
		var updates []update
		err := b.call(ctx, "getUpdates", map[string]any{
			"offset": offset, "timeout": pollTimeout,
			"allowed_updates": []string{"message", "callback_query"},
		}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if delay == 0 {
				delay = backoffBase
			} else if delay *= 2; delay > maxBackoff {
				delay = maxBackoff
			}
			b.log.Warn("telegram: getUpdates failed", "err", err, "retry_in", delay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			continue
		}
		delay = 0
		for _, u := range updates {
			b.handleUpdate(ctx, u)
			b.mu.Lock()
			b.st.Offset = u.UpdateID + 1
			b.save()
			b.mu.Unlock()
		}
	}
}

// Wire types for the parts of the Bot API we read.
type update struct {
	UpdateID      int64     `json:"update_id"`
	Message       *message  `json:"message"`
	CallbackQuery *callback `json:"callback_query"`
}

type message struct {
	MessageID int64  `json:"message_id"`
	From      *user  `json:"from"`
	Chat      chat   `json:"chat"`
	Text      string `json:"text"`
}

type chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type user struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type callback struct {
	ID      string   `json:"id"`
	From    user     `json:"from"`
	Message *message `json:"message"`
	Data    string   `json:"data"`
}

func (b *Bot) handleUpdate(ctx context.Context, u update) {
	switch {
	case u.Message != nil:
		m := u.Message
		if m.From == nil || !b.allowed[m.From.ID] || m.Chat.Type != "private" {
			b.ignore(m.From)
			return
		}
		b.handleMessage(ctx, m)
	case u.CallbackQuery != nil:
		if !b.allowed[u.CallbackQuery.From.ID] {
			b.ignore(&u.CallbackQuery.From)
			return
		}
		b.handleCallback(ctx, u.CallbackQuery)
	}
}

func (b *Bot) ignore(from *user) {
	var id int64
	if from != nil {
		id = from.ID
	}
	b.log.Debug("telegram: ignoring update from user not allowed here", "user_id", id)
}

func (b *Bot) handleMessage(ctx context.Context, m *message) {
	chatID := m.Chat.ID
	if m.Text == "" {
		b.sendText(ctx, chatID, "Only text messages are supported.")
		return
	}
	if strings.HasPrefix(m.Text, "/") {
		cmd, _, _ := strings.Cut(strings.Fields(m.Text)[0], "@")
		switch cmd {
		case "/start":
			b.sendText(ctx, chatID, "Send me a message to chat with the onett orchestrator. /new starts a fresh conversation; approval requests arrive here with Approve and Reject buttons.")
			return
		case "/new":
			if _, err := b.newConversation(ctx, chatID); err != nil {
				b.sendText(ctx, chatID, "Could not start a conversation: "+err.Error())
				return
			}
			b.sendText(ctx, chatID, "Started a new conversation.")
			return
		}
	}
	b.mu.Lock()
	convID, ok := b.st.Chats[chatID]
	b.mu.Unlock()
	if !ok {
		conv, err := b.newConversation(ctx, chatID)
		if err != nil {
			b.sendText(ctx, chatID, "Could not start a conversation: "+err.Error())
			return
		}
		convID = conv
	}
	if _, err := b.host.SendChat(ctx, convID, m.Text); err != nil {
		b.sendText(ctx, chatID, "Could not send your message: "+err.Error())
		return
	}
	if err := b.call(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": "typing"}, nil); err != nil {
		b.log.Debug("telegram: sendChatAction failed", "err", err)
	}
}

func (b *Bot) newConversation(ctx context.Context, chatID int64) (string, error) {
	conv, err := b.host.CreateConversation(ctx, "Telegram")
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	b.st.Chats[chatID] = conv.ID
	b.save()
	b.mu.Unlock()
	return conv.ID, nil
}

func (b *Bot) handleCallback(ctx context.Context, cb *callback) {
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 3 || parts[0] != "a" || (parts[2] != "y" && parts[2] != "n") {
		b.answer(ctx, cb.ID, "Unknown action.")
		return
	}
	id, approve := parts[1], parts[2] == "y"
	who := cb.From.Username
	if who == "" {
		who = strconv.FormatInt(cb.From.ID, 10)
	}
	if _, err := b.host.DecideApproval(ctx, id, approve, "via Telegram by "+who); err != nil {
		b.answer(ctx, cb.ID, err.Error())
		return
	}
	outcome := outcomeText(approve)
	b.answer(ctx, cb.ID, outcome)
	msgs := b.takeApproval(id)
	if cb.Message != nil {
		found := false
		for _, m := range msgs {
			found = found || (m.ChatID == cb.Message.Chat.ID && m.MessageID == cb.Message.MessageID)
		}
		if !found {
			msgs = append(msgs, sentMsg{cb.Message.Chat.ID, cb.Message.MessageID, cb.Message.Text})
		}
	}
	b.editAll(ctx, msgs, outcome)
}

func outcomeText(approve bool) string {
	if approve {
		return "Approved"
	}
	return "Rejected"
}

func (b *Bot) answer(ctx context.Context, id, text string) {
	if err := b.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil); err != nil {
		b.log.Warn("telegram: answerCallbackQuery failed", "err", err)
	}
}

func (b *Bot) takeApproval(id string) []sentMsg {
	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.approvals[id]
	delete(b.approvals, id)
	return msgs
}

// editAll appends the outcome to each message and drops its keyboard (editMessageText without
// reply_markup removes it).
func (b *Bot) editAll(ctx context.Context, msgs []sentMsg, outcome string) {
	for _, m := range msgs {
		err := b.call(ctx, "editMessageText", map[string]any{
			"chat_id": m.ChatID, "message_id": m.MessageID, "text": m.Text + "\n\n" + outcome,
		}, nil)
		if err != nil {
			b.log.Warn("telegram: editMessageText failed", "err", err)
		}
	}
}

func (b *Bot) handleEvent(ctx context.Context, ev domain.Event) {
	switch ev.Type {
	case domain.EvChatMessage:
		var m domain.ChatMessage
		if json.Unmarshal(ev.Data, &m) == nil {
			b.relayChat(ctx, m)
		}
	case domain.EvApprovalRequested:
		var a domain.Approval
		if json.Unmarshal(ev.Data, &a) == nil {
			b.requestApproval(ctx, a)
		}
	case domain.EvApprovalDecided:
		var a domain.Approval
		if json.Unmarshal(ev.Data, &a) == nil {
			outcome := string(a.Status)
			switch a.Status {
			case domain.ApprovalApproved:
				outcome = "Approved"
			case domain.ApprovalRejected:
				outcome = "Rejected"
			}
			b.editAll(ctx, b.takeApproval(a.ID), outcome)
		}
	}
}

func (b *Bot) relayChat(ctx context.Context, m domain.ChatMessage) {
	if m.Role != "assistant" && m.Role != "system" {
		return
	}
	b.mu.Lock()
	var chats []int64
	for c, conv := range b.st.Chats {
		if conv == m.ConversationID {
			chats = append(chats, c)
		}
	}
	if len(chats) == 0 || b.seen[m.ID] {
		b.mu.Unlock()
		return
	}
	b.seen[m.ID] = true
	b.seenOrder = append(b.seenOrder, m.ID)
	if len(b.seenOrder) > seenLimit {
		delete(b.seen, b.seenOrder[0])
		b.seenOrder = b.seenOrder[1:]
	}
	b.mu.Unlock()
	for _, c := range chats {
		b.sendText(ctx, c, m.Text)
	}
}

func (b *Bot) requestApproval(ctx context.Context, a domain.Approval) {
	b.mu.Lock()
	var targets []int64
	for c := range b.st.Chats {
		targets = append(targets, c)
	}
	b.mu.Unlock()
	if len(targets) == 0 {
		// A private chat's ID equals the user's ID, so we can message allowed users directly.
		for id := range b.allowed {
			targets = append(targets, id)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })

	text := "Approval needed: " + a.Title + "\n" + a.Reason
	params := func(chatID int64) map[string]any {
		p := map[string]any{"chat_id": chatID, "text": text}
		y, n := "a:"+a.ID+":y", "a:"+a.ID+":n"
		if len(y) <= 64 { // callback_data limit; otherwise decide in the web UI
			p["reply_markup"] = map[string]any{"inline_keyboard": [][]map[string]string{{
				{"text": "Approve", "callback_data": y}, {"text": "Reject", "callback_data": n},
			}}}
		}
		return p
	}
	for _, c := range targets {
		var sent struct {
			MessageID int64 `json:"message_id"`
		}
		if err := b.call(ctx, "sendMessage", params(c), &sent); err != nil {
			b.log.Warn("telegram: approval message failed", "chat_id", c, "err", err)
			continue
		}
		b.mu.Lock()
		b.approvals[a.ID] = append(b.approvals[a.ID], sentMsg{c, sent.MessageID, text})
		b.mu.Unlock()
	}
}

var _ channel.Tester = (*Bot)(nil)

// SendTest messages every allowed user directly (a private chat's ID is the user's ID). It
// reports the first failure, which carries no token.
func (b *Bot) SendTest(ctx context.Context) error {
	ids := make([]int64, 0, len(b.allowed))
	for id := range b.allowed {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var errs []error
	for _, id := range ids {
		if err := b.call(ctx, "sendMessage", map[string]any{"chat_id": id, "text": channel.TestMessage}, nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sendText sends plain text, split into Telegram-sized pieces.
func (b *Bot) sendText(ctx context.Context, chatID int64, text string) {
	for _, part := range splitText(text, maxText) {
		if err := b.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": part}, nil); err != nil {
			b.log.Warn("telegram: sendMessage failed", "err", err)
			return
		}
	}
}

// splitText cuts text into pieces of at most limit runes, preferring to break at a newline.
func splitText(text string, limit int) []string {
	r := []rune(text)
	var out []string
	for len(r) > limit {
		cut := limit
		for i := limit - 1; i > 0; i-- {
			if r[i] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
		if len(r) > 0 && r[0] == '\n' && cut < limit {
			r = r[1:]
		}
	}
	if len(r) > 0 || len(out) == 0 {
		out = append(out, string(r))
	}
	return out
}

// call posts one Bot API method. Errors never carry the request URL, which holds the token.
func (b *Bot) call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.api+"/bot"+b.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: bad request", method)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram %s: %s", method, b.scrub(err.Error()))
	}
	defer resp.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&env); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d, unreadable response", method, resp.StatusCode)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: HTTP %d: %s", method, resp.StatusCode, b.scrub(env.Description))
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("telegram %s: unexpected result", method)
		}
	}
	return nil
}

// scrub is a last line of defence in case a message ever echoes the token.
func (b *Bot) scrub(s string) string {
	return strings.ReplaceAll(s, b.token, "[redacted]")
}
