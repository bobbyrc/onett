package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bobbyrc/onett/internal/domain"
)

// maxResultSize caps result.json. A report is a few paragraphs of text; a file this large is not
// one.
const maxResultSize = 1 << 20

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
//
// Strict: the file (see readReport), the document (one JSON object with nothing after it, and
// a value of the wrong type anywhere voids it, except domain.IssueRef) and the status (one of
// the four known). Not strict, on purpose: a field onett does not know is ignored. Refusing
// unknown keys protects a person from a typo that silently does nothing. This file's writer is
// the agent, which a strict decoder cannot constrain: an agent that meant harm would simply not
// write what the decoder refuses. An unknown field can do nothing, a missing one reads as no
// approval and no risk rating, which policy treats as medium. So a mistyped key loses what it
// was meant to say (a "high" under the wrong name is rated medium, and never below what the
// risk rules say), where refusing the report would throw away a whole run.
func parseResult(dir string) (domain.RunResult, bool) {
	data, ok := readReport(filepath.Join(dir, domain.OutboxDir, domain.ResultFile))
	if !ok {
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

// readReport reads the report file. The agent chose what is at that path, so only a regular
// file of at most maxResultSize is read: a symlink in its place is not followed, and a pipe or
// a device, which could block the read for ever or never end, is refused.
func readReport(path string) ([]byte, bool) {
	// O_NONBLOCK makes opening a named pipe return at once instead of waiting for a writer.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxResultSize+1))
	if err != nil || len(data) > maxResultSize {
		return nil, false
	}
	return data, true
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
