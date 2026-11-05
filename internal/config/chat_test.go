package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatAttachmentDefaults(t *testing.T) {
	cfg, err := Parse([]byte("projects: {}\n"), "")
	require.NoError(t, err)
	a := cfg.Chat.Attachments
	assert.Equal(t, Size(25<<20), a.MaxSize)
	assert.Equal(t, 10, a.MaxPerMessage)
	assert.Equal(t, 30*24*time.Hour, a.Retention.Duration)
}

func TestChatAttachmentSettingsParse(t *testing.T) {
	cfg, err := Parse([]byte("chat:\n  attachments:\n    max_size: 5MB\n    max_per_message: 3\n    retention: 24h\n"), "")
	require.NoError(t, err)
	assert.Equal(t, Size(5<<20), cfg.Chat.Attachments.MaxSize)
	assert.Equal(t, 3, cfg.Chat.Attachments.MaxPerMessage)
	assert.Equal(t, 24*time.Hour, cfg.Chat.Attachments.Retention.Duration)

	cfg, err = Parse([]byte("chat:\n  attachments:\n    max_size: 1048576\n"), "")
	require.NoError(t, err)
	assert.Equal(t, Size(1<<20), cfg.Chat.Attachments.MaxSize)
}

func TestChatAttachmentSettingsRejectNegative(t *testing.T) {
	for key, val := range map[string]string{"max_size": "-1", "max_per_message": "-2", "retention": "-1h"} {
		_, err := Parse([]byte("chat:\n  attachments:\n    "+key+": "+val+"\n"), "")
		require.Error(t, err, key)
		assert.Contains(t, err.Error(), "chat.attachments."+key)
	}
	_, err := Parse([]byte("chat:\n  attachments:\n    max_size: lots\n"), "")
	require.Error(t, err)
}

func TestSizeRoundTrip(t *testing.T) {
	for in, want := range map[string]Size{"25MB": 25 << 20, "512k": 512 << 10, "1.5GB": 3 << 29, "100": 100, "100B": 100} {
		got, err := ParseSize(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	assert.Equal(t, "25MB", Size(25<<20).String())
	assert.Equal(t, "1536KB", Size(3<<19).String())
	assert.Equal(t, "100B", Size(100).String())
	_, err := ParseSize("1.5")
	assert.Error(t, err)
}
