package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bobbyrc/onett/internal/domain"
)

// TestResultSurvivesMistypedFrictionIssue: an agent that gets the type of the optional issue field
// wrong must lose only that claim, not its whole report.
func TestResultSurvivesMistypedFrictionIssue(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{`224`, 224},
		{`"224"`, 224},
		{`"#224"`, 224},
		{`" #224 "`, 224},
		{`224.0`, 0},
		{`"abc"`, 0},
		{`null`, 0},
		{`-5`, 0},
		{`true`, 0},
		{`[224]`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, domain.OutboxDir), 0o755); err != nil {
				t.Fatal(err)
			}
			body := `{"status":"completed","summary":"done","risk":"low","friction":[{"title":"t","body":"b","target":"onett","issue":` + tc.raw + `}]}`
			if err := os.WriteFile(filepath.Join(dir, domain.OutboxDir, domain.ResultFile), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			r, ok := parseResult(dir)
			if !ok {
				t.Fatalf("parseResult ok = false, want true: the whole report was rejected")
			}
			if len(r.Friction) != 1 {
				t.Fatalf("got %d friction entries, want 1", len(r.Friction))
			}
			if r.Friction[0].Title != "t" {
				t.Errorf("Title = %q, want %q", r.Friction[0].Title, "t")
			}
			if got := int(r.Friction[0].Issue); got != tc.want {
				t.Errorf("Issue = %d, want %d", got, tc.want)
			}
		})
	}
}
