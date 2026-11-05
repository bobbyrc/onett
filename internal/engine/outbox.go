package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bobbyrc/onett/internal/domain"
)

// readResult parses the agent's report from <dir>/.onett-run/result.json. A missing or
// unparsable report is a failed run; finalText (the agent's last message) is kept as context.
func readResult(dir string, finalText string) domain.RunResult {
	failed := func() domain.RunResult {
		msg := "agent did not write a valid result.json"
		if s := strings.TrimSpace(finalText); s != "" {
			msg += ": " + firstChars(s, 2000)
		}
		return domain.RunResult{Status: domain.ResultFailed, Summary: msg}
	}

	if r, ok := parseResult(dir); ok {
		return r
	}
	return failed()
}

// parseResult reads <dir>/.onett-run/result.json and reports whether it holds a valid report.
func parseResult(dir string) (domain.RunResult, bool) {
	data, err := os.ReadFile(filepath.Join(dir, domain.OutboxDir, domain.ResultFile))
	if err != nil {
		return domain.RunResult{}, false
	}
	var r domain.RunResult
	if err := json.Unmarshal(data, &r); err != nil {
		return domain.RunResult{}, false
	}
	switch r.Status {
	case domain.ResultCompleted, domain.ResultNoChanges, domain.ResultBlocked, domain.ResultFailed:
	default:
		return domain.RunResult{}, false
	}
	switch r.Risk {
	case domain.RiskLow, domain.RiskMedium, domain.RiskHigh:
	default:
		r.Risk = domain.RiskUnknown // policy treats unknown as medium
	}
	return r, true
}

// withReport appends the agent's report, if it wrote a valid one, to the reason a run was
// killed (stalled or timed out), so the owner sees what the agent said. It returns reason
// unchanged when there is no valid report.
func withReport(dir, reason string) string {
	r, ok := parseResult(dir)
	if !ok {
		return reason
	}
	text := r.Summary
	if r.Status == domain.ResultBlocked && strings.TrimSpace(r.BlockedReason) != "" {
		text = r.BlockedReason
		if strings.TrimSpace(r.Summary) != "" {
			text = r.Summary + " (" + r.BlockedReason + ")"
		}
	}
	return reason + ", after the agent reported: " + string(r.Status) + ": " + firstChars(strings.TrimSpace(text), 500)
}

// frictionFingerprint dedupes friction reports: the first 16 hex characters of the SHA-256 of
// the title, lowercased, with runs of spaces collapsed and the ends trimmed.
func frictionFingerprint(title string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(title)), " ")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])[:16]
}

// firstChars returns at most n characters from the start of s.
func firstChars(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// lastChars returns at most n characters from the end of s.
func lastChars(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
