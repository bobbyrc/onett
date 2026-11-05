package channel_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/channel/telegram"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

const token = "1:TESTTOKEN"

type fakeController struct {
	mu   sync.Mutex
	sent [][2]string
}

func (c *fakeController) SendChat(_ context.Context, conv, text string) (domain.ChatMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, [2]string{conv, text})
	return domain.ChatMessage{ConversationID: conv, Text: text}, nil
}

func (c *fakeController) SendChatWith(ctx context.Context, conv, text string, _ []string) (domain.ChatMessage, error) {
	return c.SendChat(ctx, conv, text)
}

func (c *fakeController) DecideApproval(_ context.Context, id string, _ bool, _ string) (domain.Approval, error) {
	return domain.Approval{ID: id}, nil
}

func (c *fakeController) messages() [][2]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][2]string(nil), c.sent...)
}

// fakeTelegram answers getUpdates with one private message from user 42, once.
func fakeTelegram(t *testing.T) *httptest.Server {
	t.Helper()
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		method := strings.TrimPrefix(r.URL.Path, "/bot"+token+"/")
		switch method {
		case "getUpdates":
			sent := false
			once.Do(func() {
				sent = true
				io.WriteString(w, `{"ok":true,"result":[{"update_id":1,"message":{"message_id":1,"from":{"id":42},"chat":{"id":42,"type":"private"},"text":"hello onett"}}]}`)
			})
			if !sent {
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Millisecond):
				}
				io.WriteString(w, `{"ok":true,"result":[]}`)
			}
		case "sendMessage":
			io.WriteString(w, `{"ok":true,"result":{"message_id":9}}`)
		default:
			io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBotOverHostCreatesConversationAndStopsOnCancel(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "onett.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctl := &fakeController{}
	host := &channel.EngineHost{Controller: ctl, Store: st, Bus: events.NewBus(st, logger)}
	api := fakeTelegram(t)

	bot, err := telegram.New(telegram.Options{
		Token: token, AllowedUserIDs: []int64{42}, APIURL: api.URL,
		StateFile: filepath.Join(t.TempDir(), "tg.json"),
	}, host, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()

	require.Eventually(t, func() bool { return len(ctl.messages()) == 1 }, 5*time.Second, 5*time.Millisecond)
	msg := ctl.messages()[0]
	assert.Equal(t, "hello onett", msg[1])
	conv, err := st.GetConversation(context.Background(), msg[0])
	require.NoError(t, err, "SendChat must target a conversation the store created")
	assert.NotEmpty(t, conv.ID)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("bot did not stop after cancel")
	}
}
