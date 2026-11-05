package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/client"
)

func TestWriteStatusShowsWhoPaused(t *testing.T) {
	since := time.Date(2026, 11, 4, 22, 36, 0, 0, time.Local)
	pause := client.PauseView{Reason: "owner", By: "bob", Since: since}
	var buf bytes.Buffer
	snap := statusSnapshot{
		Stats:   client.Stats{Paused: true, Pause: &pause},
		Control: client.ControlState{Paused: true, Pause: &pause},
	}
	if err := writeStatus(&buf, snap); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(buf.String(), "\n")
	if !strings.HasPrefix(lines[0], "Factory: PAUSED.") || lines[1] != "Paused by bob since 22:36." {
		t.Errorf("unexpected status:\n%s", buf.String())
	}
}
