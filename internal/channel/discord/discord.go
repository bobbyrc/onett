// Package discord is a two-way chat channel between the owner and onett over a Discord bot.
//
// The bot connects out to the Discord Gateway (a websocket), so no public URL or interaction
// webhook is needed. Only the Discord user IDs in the allowlist are obeyed, and only in direct
// messages: an allowed user can approve merges, so this is the security boundary and everything
// else is dropped. Each DM channel maps to one orchestrator conversation. Replies and approval
// requests arrive from the daemon's event stream, which is read in its own goroutine so a slow
// Discord call can never stall the daemon; Gateway events are handled in another goroutine so a
// slow handler can never delay a heartbeat.
//
// The bot owns one small JSON state file (the DM-channel-to-conversation map) so a restart does
// not lose conversations. The bot token travels only in the Authorization header and the
// Gateway IDENTIFY frame, never in a URL, and errors are scrubbed of it before they are returned
// or logged; the state file never holds it.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/domain"
)

const (
	defaultAPI = "https://discord.com/api/v10"
	// maxText is Discord's message length limit, counted in characters.
	maxText = 2000
	// intentDirectMessages is the only Gateway intent needed: bots receive the content of DMs
	// without the privileged MESSAGE_CONTENT intent.
	intentDirectMessages = 1 << 12
	// seenLimit bounds the set of chat message IDs already relayed.
	seenLimit = 500
	// eventBuffer is how many events may queue between a reader and its handler.
	eventBuffer = 256
	maxBackoff  = 60 * time.Second
	// customIDLimit is Discord's limit on a button's custom_id.
	customIDLimit = 100
)

// backoffBase is the first retry delay after a Gateway error; tests shorten it.
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
	AllowedUserIDs []string     // Discord user IDs (snowflakes) allowed to talk to onett; must be non-empty
	StateFile      string       // JSON file the bot owns, e.g. <data_dir>/channels/discord.json
	APIURL         string       // REST base, default "https://discord.com/api/v10"
	GatewayURL     string       // optional Gateway override; default comes from GET /gateway/bot
	HTTPClient     *http.Client // nil = default with a timeout
}

// Bot is the Discord channel. Create it with New and run it with Run.
type Bot struct {
	token   string
	api     string
	gateway string
	allowed map[string]bool
	file    string
	client  *http.Client
	host    Host
	log     *slog.Logger

	inbound chan dispatch // Gateway events waiting for the handler goroutine

	mu         sync.Mutex
	st         state
	botID      string // our own user ID, from READY
	sessionID  string // Gateway session, for RESUME
	resumeURL  string
	seq        *int64
	approvals  map[string][]sentMsg // approval ID -> messages carrying its buttons
	seen       map[string]bool
	seenOrder  []string
	warnedUser map[string]bool
}

// state is what survives a restart.
type state struct {
	Channels map[string]string `json:"channels"` // DM channel ID -> conversation ID
	Users    map[string]string `json:"users"`    // DM channel ID -> user ID it belongs to
}

type sentMsg struct {
	ChannelID string
	MessageID string
	Text      string
}

type dispatch struct {
	Type string
	Data json.RawMessage
}

// New validates the options and loads the state file.
func New(opts Options, host Host, logger *slog.Logger) (*Bot, error) {
	if opts.Token == "" {
		return nil, errors.New("discord: token is required")
	}
	if len(opts.AllowedUserIDs) == 0 {
		return nil, errors.New("discord: allowed_user_ids must not be empty")
	}
	if opts.StateFile == "" {
		return nil, errors.New("discord: state file is required")
	}
	if host == nil {
		return nil, errors.New("discord: host is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bot{
		token: opts.Token, api: strings.TrimRight(opts.APIURL, "/"), gateway: opts.GatewayURL,
		file: opts.StateFile, client: opts.HTTPClient, host: host, log: logger,
		allowed:    map[string]bool{},
		approvals:  map[string][]sentMsg{},
		seen:       map[string]bool{},
		warnedUser: map[string]bool{},
		inbound:    make(chan dispatch, eventBuffer),
	}
	if b.api == "" {
		b.api = defaultAPI
	}
	if b.client == nil {
		b.client = &http.Client{Timeout: 30 * time.Second}
	}
	for _, id := range opts.AllowedUserIDs {
		if id == "" {
			return nil, errors.New("discord: allowed_user_ids must not contain an empty ID")
		}
		b.allowed[id] = true
	}
	if err := b.load(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Bot) load() error {
	b.st = state{Channels: map[string]string{}, Users: map[string]string{}}
	data, err := os.ReadFile(b.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("discord: read state: %w", err)
	}
	if err := json.Unmarshal(data, &b.st); err != nil {
		return fmt.Errorf("discord: parse state file: %w", err)
	}
	if b.st.Channels == nil {
		b.st.Channels = map[string]string{}
	}
	if b.st.Users == nil {
		b.st.Users = map[string]string{}
	}
	return nil
}

// save writes the state atomically. The caller holds b.mu.
func (b *Bot) save() {
	if err := b.writeState(); err != nil {
		b.log.Warn("discord: save state failed", "err", err)
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
	tmp, err := os.CreateTemp(dir, ".discord-*.tmp")
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

// Run keeps a Gateway session open and relays events until ctx is done. It returns nil on
// cancel, and an error only when Discord refuses the bot for good (a bad token or intents).
func (b *Bot) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	events, unsub := b.host.Subscribe()
	defer unsub()

	// The reader never waits on Discord: it hands events to the sender through a buffer and
	// drops them (with a warning) if the buffer is full.
	queue := make(chan domain.Event, eventBuffer)
	var wg sync.WaitGroup
	wg.Add(3)
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
					b.log.Warn("discord: event queue full, dropping event", "type", ev.Type)
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
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case d := <-b.inbound:
				b.handleDispatch(ctx, d)
			}
		}
	}()

	err := b.connectLoop(ctx)
	cancel()
	wg.Wait()
	return err
}

// fatalError marks a Gateway refusal that retrying cannot fix.
type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

// connectLoop opens Gateway sessions, one after another, with backoff between failures.
func (b *Bot) connectLoop(ctx context.Context) error {
	delay := time.Duration(0)
	for ctx.Err() == nil {
		ready, err := b.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		var fe *fatalError
		if errors.As(err, &fe) {
			return fmt.Errorf("discord: %s", b.scrub(fe.msg))
		}
		if ready {
			delay = 0
		}
		if delay == 0 {
			delay = backoffBase
		} else if delay *= 2; delay > maxBackoff {
			delay = maxBackoff
		}
		b.log.Warn("discord: gateway session ended", "err", b.scrubErr(err), "retry_in", delay)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
	return nil
}

// gatewayFrame is one Gateway message.
type gatewayFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

// session runs one websocket connection. ready reports whether it got as far as READY/RESUMED,
// which resets the reconnect backoff.
func (b *Bot) session(ctx context.Context) (ready bool, err error) {
	wsURL, err := b.gatewayURL(ctx)
	if err != nil {
		return false, err
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, _, err := websocket.Dial(sctx, wsURL, nil)
	if err != nil {
		return false, fmt.Errorf("gateway dial: %s", b.scrub(err.Error()))
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)

	// When ctx ends, say goodbye with a close frame instead of dropping the connection. The read
	// loop keeps running (on its own context) so it can receive the peer's answering frame; any
	// other end of the session just cancels the read.
	rctx, rcancel := context.WithCancel(context.Background())
	defer rcancel()
	go func() {
		<-sctx.Done()
		if ctx.Err() != nil {
			conn.Close(websocket.StatusNormalClosure, "bye")
		}
		rcancel()
	}()

	acked := true
	var ackMu sync.Mutex
	setAcked := func(v bool) { ackMu.Lock(); acked = v; ackMu.Unlock() }
	getAcked := func() bool { ackMu.Lock(); defer ackMu.Unlock(); return acked }

	for {
		_, data, err := conn.Read(rctx)
		if err != nil {
			if code := websocket.CloseStatus(err); code != -1 {
				switch code {
				case 4004, 4010, 4011, 4012, 4013, 4014:
					return ready, &fatalError{fmt.Sprintf("gateway refused the bot (close code %d)", code)}
				case 4007, 4009:
					b.clearSession()
				}
				return ready, fmt.Errorf("gateway closed (code %d)", code)
			}
			return ready, fmt.Errorf("gateway read: %s", b.scrub(err.Error()))
		}
		var f gatewayFrame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.Op {
		case 10: // HELLO
			var h struct {
				Interval int `json:"heartbeat_interval"`
			}
			if json.Unmarshal(f.D, &h) != nil || h.Interval <= 0 {
				return ready, errors.New("gateway sent a bad HELLO")
			}
			interval := time.Duration(h.Interval) * time.Millisecond
			go b.heartbeat(sctx, conn, interval, setAcked, getAcked, cancel)
			if err := b.identifyOrResume(sctx, conn); err != nil {
				return ready, fmt.Errorf("gateway identify: %s", b.scrub(err.Error()))
			}
		case 11: // heartbeat ACK
			setAcked(true)
		case 1: // the server asks for a heartbeat now
			if err := b.sendHeartbeat(sctx, conn); err != nil {
				return ready, err
			}
		case 7: // RECONNECT: resume on a fresh connection
			return ready, errors.New("gateway asked us to reconnect")
		case 9: // INVALID_SESSION; d says whether the session can still be resumed
			var resumable bool
			_ = json.Unmarshal(f.D, &resumable)
			if !resumable {
				b.clearSession()
			}
			return ready, errors.New("gateway session invalidated")
		case 0:
			if f.S != nil {
				b.mu.Lock()
				s := *f.S
				b.seq = &s
				b.mu.Unlock()
			}
			switch f.T {
			case "READY":
				var r struct {
					SessionID string `json:"session_id"`
					ResumeURL string `json:"resume_gateway_url"`
					User      struct {
						ID string `json:"id"`
					} `json:"user"`
				}
				if json.Unmarshal(f.D, &r) != nil {
					return ready, errors.New("gateway sent a bad READY")
				}
				b.mu.Lock()
				b.sessionID, b.resumeURL, b.botID = r.SessionID, r.ResumeURL, r.User.ID
				b.mu.Unlock()
				ready = true
				b.log.Info("discord: connected")
			case "RESUMED":
				ready = true
				b.log.Info("discord: resumed")
			default:
				select {
				case b.inbound <- dispatch{f.T, f.D}:
				default:
					b.log.Warn("discord: gateway queue full, dropping event", "type", f.T)
				}
			}
		}
	}
}

func (b *Bot) clearSession() {
	b.mu.Lock()
	b.sessionID, b.resumeURL, b.seq = "", "", nil
	b.mu.Unlock()
}

// gatewayURL picks where to connect: the resume URL when a session can be resumed, else the
// override, else whatever GET /gateway/bot says.
func (b *Bot) gatewayURL(ctx context.Context) (string, error) {
	b.mu.Lock()
	resume := b.resumeURL
	b.mu.Unlock()
	base := b.gateway
	if base == "" {
		base = resume
	}
	if base == "" {
		var out struct {
			URL string `json:"url"`
		}
		if err := b.call(ctx, http.MethodGet, "/gateway/bot", nil, &out); err != nil {
			return "", err
		}
		base = out.URL
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", errors.New("discord: bad gateway URL")
	}
	q := u.Query()
	q.Set("v", "10")
	q.Set("encoding", "json")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (b *Bot) writeFrame(ctx context.Context, conn *websocket.Conn, op int, d any) error {
	data, err := json.Marshal(map[string]any{"op": op, "d": d})
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func (b *Bot) identifyOrResume(ctx context.Context, conn *websocket.Conn) error {
	b.mu.Lock()
	sid, seq := b.sessionID, b.seq
	b.mu.Unlock()
	if sid != "" && seq != nil {
		return b.writeFrame(ctx, conn, 6, map[string]any{"token": b.token, "session_id": sid, "seq": *seq})
	}
	return b.writeFrame(ctx, conn, 2, map[string]any{
		"token": b.token, "intents": intentDirectMessages,
		"properties": map[string]string{"os": "onett", "browser": "onett", "device": "onett"},
	})
}

func (b *Bot) sendHeartbeat(ctx context.Context, conn *websocket.Conn) error {
	b.mu.Lock()
	var d any
	if b.seq != nil {
		d = *b.seq
	}
	b.mu.Unlock()
	return b.writeFrame(ctx, conn, 1, d)
}

// heartbeat beats every interval (the first one after a random fraction of it, as Discord asks).
// A beat that was never acknowledged means the connection is a zombie: drop it so the loop
// reconnects.
func (b *Bot) heartbeat(ctx context.Context, conn *websocket.Conn, interval time.Duration, setAcked func(bool), getAcked func() bool, stop context.CancelFunc) {
	wait := time.Duration(rand.Float64() * float64(interval))
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = interval
		if !getAcked() {
			b.log.Warn("discord: heartbeat not acknowledged, reconnecting")
			conn.CloseNow()
			stop()
			return
		}
		setAcked(false)
		if err := b.sendHeartbeat(ctx, conn); err != nil {
			stop()
			return
		}
	}
}

// Wire types for the parts of the Discord API we read.
type user struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

type message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	Author    user   `json:"author"`
	Content   string `json:"content"`
}

type interaction struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	Type      int    `json:"type"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	User      *user  `json:"user"` // set in DMs; guild interactions carry member.user
	Member    *struct {
		User user `json:"user"`
	} `json:"member"`
	Data struct {
		CustomID string `json:"custom_id"`
	} `json:"data"`
	Message *message `json:"message"`
}

func (b *Bot) handleDispatch(ctx context.Context, d dispatch) {
	switch d.Type {
	case "MESSAGE_CREATE":
		var m message
		if json.Unmarshal(d.Data, &m) != nil {
			return
		}
		b.mu.Lock()
		self := b.botID
		b.mu.Unlock()
		if m.GuildID != "" || m.Author.Bot || m.Author.ID == self {
			return
		}
		if !b.allowed[m.Author.ID] {
			b.ignore(m.Author.ID)
			return
		}
		b.handleMessage(ctx, m)
	case "INTERACTION_CREATE":
		var in interaction
		if json.Unmarshal(d.Data, &in) != nil {
			return
		}
		b.handleInteraction(ctx, in)
	}
}

// ignore logs a user who is not allowed here once, without anything they wrote.
func (b *Bot) ignore(userID string) {
	b.mu.Lock()
	seen := b.warnedUser[userID]
	b.warnedUser[userID] = true
	b.mu.Unlock()
	if !seen {
		b.log.Warn("discord: ignoring user not on the allowlist", "user_id", userID)
	}
}

func (b *Bot) handleMessage(ctx context.Context, m message) {
	ch := m.ChannelID
	if m.Content == "" {
		b.sendText(ctx, ch, "Only text messages are supported.")
		return
	}
	switch strings.Fields(m.Content)[0] {
	case "/start":
		b.sendText(ctx, ch, "Send me a message to chat with the onett orchestrator. /new starts a fresh conversation; approval requests arrive here with Approve and Reject buttons.")
		return
	case "/new":
		if _, err := b.newConversation(ctx, ch, m.Author.ID); err != nil {
			b.sendText(ctx, ch, "Could not start a conversation: "+err.Error())
			return
		}
		b.sendText(ctx, ch, "Started a new conversation.")
		return
	}
	b.mu.Lock()
	convID, ok := b.st.Channels[ch]
	b.mu.Unlock()
	if !ok {
		conv, err := b.newConversation(ctx, ch, m.Author.ID)
		if err != nil {
			b.sendText(ctx, ch, "Could not start a conversation: "+err.Error())
			return
		}
		convID = conv
	}
	if _, err := b.host.SendChat(ctx, convID, m.Content); err != nil {
		b.sendText(ctx, ch, "Could not send your message: "+err.Error())
		return
	}
	if err := b.call(ctx, http.MethodPost, "/channels/"+ch+"/typing", nil, nil); err != nil {
		b.log.Debug("discord: typing indicator failed", "err", err)
	}
}

func (b *Bot) newConversation(ctx context.Context, channelID, userID string) (string, error) {
	conv, err := b.host.CreateConversation(ctx, "Discord")
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	b.st.Channels[channelID] = conv.ID
	b.st.Users[channelID] = userID
	b.save()
	b.mu.Unlock()
	return conv.ID, nil
}

func (b *Bot) handleInteraction(ctx context.Context, in interaction) {
	if in.Type != 3 { // message component (a button click)
		return
	}
	var from user
	switch {
	case in.User != nil:
		from = *in.User
	case in.Member != nil:
		from = in.Member.User
	}
	if !b.allowed[from.ID] {
		b.ignore(from.ID)
		b.respond(ctx, in, 4, map[string]any{"content": "You are not allowed to do that.", "flags": 64})
		return
	}
	parts := strings.Split(in.Data.CustomID, ":")
	if len(parts) != 3 || parts[0] != "a" || (parts[2] != "y" && parts[2] != "n") {
		b.respond(ctx, in, 4, map[string]any{"content": "Unknown action.", "flags": 64})
		return
	}
	id, approve := parts[1], parts[2] == "y"
	who := from.Username
	if who == "" {
		who = from.ID
	}
	if _, err := b.host.DecideApproval(ctx, id, approve, "via Discord by "+who); err != nil {
		b.respond(ctx, in, 4, map[string]any{"content": truncate(err.Error(), 500), "flags": 64})
		return
	}
	outcome := outcomeText(approve)
	msgs := b.takeApproval(id)
	var clicked *sentMsg
	var others []sentMsg
	for i := range msgs {
		if in.Message != nil && msgs[i].ChannelID == in.Message.ChannelID && msgs[i].MessageID == in.Message.ID {
			clicked = &msgs[i]
			continue
		}
		others = append(others, msgs[i])
	}
	if in.Message != nil && clicked == nil {
		clicked = &sentMsg{in.Message.ChannelID, in.Message.ID, in.Message.Content}
	}
	// Answer the click by rewriting the clicked message in place (callback type 7), which also
	// drops its buttons.
	text := outcome
	if clicked != nil {
		text = truncate(clicked.Text, maxText-len(outcome)-2) + "\n\n" + outcome
	}
	b.respond(ctx, in, 7, map[string]any{"content": text, "components": []any{}})
	b.editAll(ctx, others, outcome)
}

// respond answers an interaction through its one-shot callback URL. The interaction token in the
// path is not the bot token.
func (b *Bot) respond(ctx context.Context, in interaction, typ int, data map[string]any) {
	path := "/interactions/" + in.ID + "/" + in.Token + "/callback"
	if err := b.call(ctx, http.MethodPost, path, map[string]any{"type": typ, "data": data}, nil); err != nil {
		b.log.Warn("discord: interaction callback failed", "err", err)
	}
}

func outcomeText(approve bool) string {
	if approve {
		return "Approved"
	}
	return "Rejected"
}

func (b *Bot) takeApproval(id string) []sentMsg {
	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.approvals[id]
	delete(b.approvals, id)
	return msgs
}

// editAll appends the outcome to each message and drops its buttons.
func (b *Bot) editAll(ctx context.Context, msgs []sentMsg, outcome string) {
	for _, m := range msgs {
		text := truncate(m.Text, maxText-len(outcome)-2) + "\n\n" + outcome
		err := b.call(ctx, http.MethodPatch, "/channels/"+m.ChannelID+"/messages/"+m.MessageID,
			map[string]any{"content": text, "components": []any{}}, nil)
		if err != nil {
			b.log.Warn("discord: edit message failed", "err", err)
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
	var chans []string
	for c, conv := range b.st.Channels {
		if conv == m.ConversationID && b.allowed[b.st.Users[c]] {
			chans = append(chans, c)
		}
	}
	if len(chans) == 0 || b.seen[m.ID] {
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
	sort.Strings(chans)
	for _, c := range chans {
		b.sendText(ctx, c, m.Text)
	}
}

// approvalTargets lists the DM channels that should get an approval: the known channels of
// allowed users, plus a freshly opened DM for each allowed user without one.
func (b *Bot) approvalTargets(ctx context.Context) []string {
	b.mu.Lock()
	var targets []string
	have := map[string]bool{}
	for c, u := range b.st.Users {
		if b.allowed[u] {
			targets = append(targets, c)
			have[u] = true
		}
	}
	var missing []string
	for u := range b.allowed {
		if !have[u] {
			missing = append(missing, u)
		}
	}
	b.mu.Unlock()
	sort.Strings(missing)
	for _, u := range missing {
		var ch struct {
			ID string `json:"id"`
		}
		if err := b.call(ctx, http.MethodPost, "/users/@me/channels", map[string]any{"recipient_id": u}, &ch); err != nil || ch.ID == "" {
			b.log.Warn("discord: could not open a DM", "user_id", u, "err", err)
			continue
		}
		targets = append(targets, ch.ID)
	}
	sort.Strings(targets)
	return targets
}

func (b *Bot) requestApproval(ctx context.Context, a domain.Approval) {
	text := truncate("Approval needed: "+a.Title+"\n"+a.Reason, maxText)
	body := map[string]any{"content": text}
	y, n := "a:"+a.ID+":y", "a:"+a.ID+":n"
	if len(y) <= customIDLimit { // otherwise decide in the web UI
		body["components"] = []any{map[string]any{"type": 1, "components": []any{
			map[string]any{"type": 2, "style": 3, "label": "Approve", "custom_id": y},
			map[string]any{"type": 2, "style": 4, "label": "Reject", "custom_id": n},
		}}}
	}
	for _, c := range b.approvalTargets(ctx) {
		var sent struct {
			ID string `json:"id"`
		}
		if err := b.call(ctx, http.MethodPost, "/channels/"+c+"/messages", body, &sent); err != nil {
			b.log.Warn("discord: approval message failed", "channel_id", c, "err", err)
			continue
		}
		b.mu.Lock()
		b.approvals[a.ID] = append(b.approvals[a.ID], sentMsg{c, sent.ID, text})
		b.mu.Unlock()
	}
}

var _ channel.Tester = (*Bot)(nil)

// SendTest messages every allowed user in a direct message, opening one when there is none. It
// reports the first failure, which carries no token.
func (b *Bot) SendTest(ctx context.Context) error {
	targets := b.approvalTargets(ctx)
	if len(targets) == 0 {
		return errors.New("discord: could not open a direct message with any allowed user (the bot must share a server with you, and your DMs must allow it)")
	}
	body := map[string]any{"content": channel.TestMessage, "allowed_mentions": map[string]any{"parse": []string{}}}
	var errs []error
	for _, c := range targets {
		if err := b.call(ctx, http.MethodPost, "/channels/"+c+"/messages", body, nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sendText sends plain text, split into Discord-sized pieces. Mentions are disabled so agent
// text can never ping anyone.
func (b *Bot) sendText(ctx context.Context, channelID, text string) {
	for _, part := range splitText(text, maxText) {
		body := map[string]any{"content": part, "allowed_mentions": map[string]any{"parse": []string{}}}
		if err := b.call(ctx, http.MethodPost, "/channels/"+channelID+"/messages", body, nil); err != nil {
			b.log.Warn("discord: send message failed", "err", err)
			return
		}
	}
}

func truncate(s string, limit int) string {
	if limit < 0 {
		limit = 0
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	if limit == 0 {
		return ""
	}
	return string(r[:limit-1]) + "…"
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

// call sends one REST request. A 429 is retried once after the wait Discord asks for. Errors
// carry no request header and are scrubbed of the token.
func (b *Bot) call(ctx context.Context, method, path string, params any, out any) error {
	var body []byte
	if params != nil {
		var err error
		if body, err = json.Marshal(params); err != nil {
			return fmt.Errorf("discord %s: %w", path, err)
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, b.api+path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("discord %s: bad request", method)
		}
		req.Header.Set("Authorization", "Bot "+b.token)
		req.Header.Set("User-Agent", "DiscordBot (https://github.com/bobbyrc/onett, 1)")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := b.client.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return fmt.Errorf("discord %s: %s", method, b.scrub(err.Error()))
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			var rl struct {
				RetryAfter float64 `json:"retry_after"`
			}
			_ = json.Unmarshal(raw, &rl)
			wait := time.Duration(rl.RetryAfter * float64(time.Second))
			if wait <= 0 || wait > 10*time.Second {
				wait = time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode >= 300 {
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			return fmt.Errorf("discord %s: HTTP %d: %s", method, resp.StatusCode, b.scrub(truncate(e.Message, 200)))
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("discord %s: unexpected response", method)
			}
		}
		return nil
	}
}

// scrub is a last line of defence in case a message ever echoes the token.
func (b *Bot) scrub(s string) string {
	return strings.ReplaceAll(s, b.token, "[redacted]")
}

func (b *Bot) scrubErr(err error) string {
	if err == nil {
		return ""
	}
	return b.scrub(err.Error())
}
