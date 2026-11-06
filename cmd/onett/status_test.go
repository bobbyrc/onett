package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/bobbyrc/onett/internal/domain"
)

func TestWriteStatusIdle(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStatus(&buf, statusSnapshot{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Factory: running.", "Tasks: -", "Cost today: $0.00", "Needs you:\n  nothing", "Running:\n  nothing"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteStatusBusy(t *testing.T) {
	snap := statusSnapshot{
		Stats: client.Stats{
			TasksByStatus: map[string]int{"blocked": 1, "queued": 0},
			ActiveRuns:    1, ApprovalsPending: 1,
			CostTodayUSD: 5, DailyBudgetUSD: 5,
		},
		Control:   client.ControlState{Paused: true},
		Approvals: []domain.Approval{{ID: "ap1", Kind: "merge", Title: "Add\nthing", Reason: ""}},
		Blocked:   []domain.Task{{ID: "t1", Title: "Stuck", BlockedReason: "denied tool"}},
		Running:   []domain.Run{{ID: "r1", TaskID: "t1", Role: "implementer", Project: "onett"}},
	}
	var buf bytes.Buffer
	if err := writeStatus(&buf, snap); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Factory: PAUSED.",
		"Tasks: blocked=1",
		"$5.00 of $5.00 daily budget (budget reached",
		"approval ap1  [merge] Add thing: -",
		"blocked  t1  Stuck: denied tool",
		"senior-engineer", // a stored old role name shows as its job title
		"run r1  task t1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "queued") {
		t.Errorf("zero counts should be hidden:\n%s", out)
	}
}
