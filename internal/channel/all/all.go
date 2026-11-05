// Package all links in every built-in chat channel type. Importing it registers them with
// package channel; a new channel type adds one line here and nothing in cmd/onett.
package all

import (
	_ "github.com/bobbyrc/onett/internal/channel/discord"
	_ "github.com/bobbyrc/onett/internal/channel/telegram"
)
