package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// `codex exec --json` does not print the model's context window on stdout. Codex does write it
// to the session's rollout file ($CODEX_HOME/sessions/YYYY/MM/DD/rollout-<time>-<thread>.jsonl)
// as an event_msg of type token_count, in info.model_context_window, so the harness reads it
// from there once the run has ended. Any failure just leaves the window unknown.

// rolloutWindow returns the last model_context_window in a rollout stream, or 0.
func rolloutWindow(r io.Reader) int64 {
	var window int64
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		// The file holds large prompts and tool output; skip lines that cannot match cheaply.
		if !bytes.Contains(line, []byte("model_context_window")) {
			continue
		}
		var l struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Info *struct {
					Window int64 `json:"model_context_window"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &l) != nil || l.Type != "event_msg" || l.Payload.Type != "token_count" {
			continue
		}
		if l.Payload.Info != nil && l.Payload.Info.Window > 0 {
			window = l.Payload.Info.Window
		}
	}
	return window
}

// sessionContextWindow finds the rollout file of thread sessionID under codexHome (default
// ~/.codex) and returns the context window it records, or 0.
func sessionContextWindow(codexHome, sessionID string) int64 {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return 0
	}
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0
		}
		codexHome = filepath.Join(home, ".codex")
	}
	matches, err := filepath.Glob(filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*-"+sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return 0
	}
	f, err := os.Open(matches[len(matches)-1])
	if err != nil {
		return 0
	}
	defer f.Close()
	return rolloutWindow(f)
}
