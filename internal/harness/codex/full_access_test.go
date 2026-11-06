package codex

import (
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/stretchr/testify/assert"
)

func TestBuildArgsFullAccess(t *testing.T) {
	bypassNew := []string{"--dangerously-bypass-approvals-and-sandbox"}
	bypassResume := []string{"-c", `sandbox_mode="danger-full-access"`, "-c", `approval_policy="never"`}
	defNew := []string{"--approve-for-me"}
	defResume := []string{"-c", `sandbox_mode="workspace-write"`}

	cases := []struct {
		name    string
		full    bool
		resume  bool
		spec    harness.Spec
		cfgArgs []string
		want    []string
		absent  []string
	}{
		{name: "off new", want: defNew, absent: []string{"--dangerously-bypass-approvals-and-sandbox"}},
		{name: "off resume", resume: true, want: defResume},
		{name: "on new", full: true, want: bypassNew, absent: []string{"--approve-for-me"}},
		{name: "on resume", full: true, resume: true, want: bypassResume, absent: []string{`sandbox_mode="workspace-write"`}},
		{name: "on, agent permission_mode wins", full: true, spec: harness.Spec{PermissionMode: "read-only"},
			want: []string{"--sandbox", "read-only"}, absent: []string{"--dangerously-bypass-approvals-and-sandbox"}},
		{name: "on resume, agent permission_mode wins", full: true, resume: true, spec: harness.Spec{PermissionMode: "read-only"},
			want: []string{"-c", `sandbox_mode="read-only"`}, absent: []string{`approval_policy="never"`}},
		{name: "off, agent bypass", spec: harness.Spec{PermissionMode: permBypass}, want: bypassNew},
		{name: "on, sandbox flag in args wins", full: true, cfgArgs: []string{"--sandbox", "read-only"},
			want: []string{"--sandbox", "read-only"}, absent: []string{"--dangerously-bypass-approvals-and-sandbox"}},
		{name: "on resume, sandbox config in args wins", full: true, resume: true, cfgArgs: []string{"-c", `sandbox_mode="read-only"`},
			want: []string{"-c", `sandbox_mode="read-only"`}, absent: []string{`approval_policy="never"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.spec
			spec.Workdir = "/w"
			if tc.resume {
				spec.ResumeSessionID = "s1"
			}
			args := buildArgs(harness.Config{FullAccess: tc.full, Args: tc.cfgArgs}, spec)
			assert.Subset(t, args, tc.want)
			assert.Contains(t, joined(args), joined(tc.want))
			for _, a := range tc.absent {
				assert.NotContains(t, args, a)
			}
		})
	}
}

func joined(a []string) string {
	s := ""
	for _, x := range a {
		s += x + "\x00"
	}
	return s
}
