package notify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegramSkipApprovalsDropsApprovalTagged(t *testing.T) {
	srv, got := newTelegramServer(t, 200, `{"ok":true}`)
	t.Setenv("TEST_TELEGRAM_TOKEN", telegramTestToken)
	tg, err := newTelegram(map[string]any{
		"bot_token_env": "TEST_TELEGRAM_TOKEN", "chat_id": "1", "api_url": srv.URL, "skip_approvals": true,
	}, srv.Client())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, tg.Notify(ctx, Notification{Title: "Approval needed: x", Tags: []string{TagApproval}}))
	assert.Empty(t, *got, "approval-tagged notification must be dropped")

	require.NoError(t, tg.Notify(ctx, Notification{Title: "Merged: x"}))
	require.NoError(t, tg.Notify(ctx, Notification{Title: "Blocked: y", Tags: []string{"other"}}))
	assert.Len(t, *got, 2)
}

func TestTelegramSendsApprovalsByDefault(t *testing.T) {
	srv, got := newTelegramServer(t, 200, `{"ok":true}`)
	tg := newTestTelegram(t, srv.URL, srv.Client())
	require.NoError(t, tg.Notify(context.Background(), Notification{Title: "Approval needed: x", Tags: []string{TagApproval}}))
	assert.Len(t, *got, 1)
}
