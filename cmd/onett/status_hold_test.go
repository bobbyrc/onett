package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/bobbyrc/onett/internal/domain"
)

func TestHoldLine(t *testing.T) {
	now := time.Date(2026, 11, 4, 14, 27, 0, 0, time.Local)
	h := domain.HarnessHold{Harness: "claude", Until: time.Date(2026, 11, 4, 15, 4, 0, 0, time.Local)}
	want := "Held: claude (rate limit) until 15:04, 37m left"
	if got := holdLine(h, now); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteStatusShowsHolds(t *testing.T) {
	until := time.Now().Add(37 * time.Minute)
	snap := statusSnapshot{Stats: client.Stats{Holds: []domain.HarnessHold{{Harness: "codex", Until: until}}}}
	var buf bytes.Buffer
	if err := writeStatus(&buf, snap); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Held: codex (rate limit) until "+until.Format("15:04")) {
		t.Errorf("status does not show the hold:\n%s", buf.String())
	}
	buf.Reset()
	if err := writeStatus(&buf, statusSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Held:") {
		t.Errorf("status shows a hold when there is none:\n%s", buf.String())
	}
}
