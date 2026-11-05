package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/client"
)

func TestWriteStatusShowsPauseReason(t *testing.T) {
	since := time.Date(2026, 11, 4, 22, 36, 0, 0, time.Local)
	cases := map[string]struct {
		pause client.PauseView
		want  string // the line after "Factory: PAUSED."
	}{
		"self-update": {
			client.PauseView{Reason: "self-update", Detail: "69f1660", Since: since,
				Waiting: []client.PauseWait{{RunID: "r1", TaskID: "01a1a9c6-1111-2222"}}},
			"Paused for self-update to 69f1660 since 22:36, waiting for 1 run (task 01a1a9c6…).",
		},
		"owner": {
			client.PauseView{Reason: "owner", By: "bob", Since: since},
			"Paused by bob since 22:36.",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			snap := statusSnapshot{
				Stats:   client.Stats{Paused: true, Pause: &tc.pause},
				Control: client.ControlState{Paused: true, Pause: &tc.pause},
			}
			if err := writeStatus(&buf, snap); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(buf.String(), "\n")
			if !strings.HasPrefix(lines[0], "Factory: PAUSED.") || lines[1] != tc.want {
				t.Errorf("want second line %q, got:\n%s", tc.want, buf.String())
			}
		})
	}
}
