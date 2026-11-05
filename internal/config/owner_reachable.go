package config

// OwnerUnreachableMessage is what status, doctor and the UI say when onett has no way to tell
// the owner it needs them.
const OwnerUnreachableMessage = "Only the log notifier is configured, so onett cannot tell you when it needs you. " +
	"Set up a channel or notifier (Telegram, Discord or ntfy): see docs/channels.md."

// OwnerReachable reports whether onett has any way to tell the owner something. It does when a
// notifier other than "log" is configured or any two-way channel is. Call it on a config after
// defaults, where an empty notify list has become [{type: log}].
func (c *Config) OwnerReachable() bool {
	if len(c.Channels) > 0 {
		return true
	}
	for _, n := range c.Notify {
		if n.Type != "log" {
			return true
		}
	}
	return false
}
