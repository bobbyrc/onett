package domain

import "time"

// Preflight is the daemon's last check that one project's tool directories can be read. A read
// that blocks (a removable volume the service has no access to) hangs every agent run, so the
// scheduler starts no run for a project while its preflight fails.
type Preflight struct {
	Project   string    `json:"project"`
	OK        bool      `json:"ok"`
	Path      string    `json:"path,omitempty"`  // the directory that failed, as configured
	Error     string    `json:"error,omitempty"` // what went wrong: an error or a timeout
	Fix       string    `json:"fix,omitempty"`   // what the owner can do about it
	CheckedAt time.Time `json:"checked_at"`
}

// Summary is the one-line text shown in notifications and `onett status`.
func (p Preflight) Summary() string {
	return "cannot read " + p.Path + " (" + p.Error + "): " + p.Fix
}
