package claude

import (
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/stretchr/testify/assert"
)

func TestBuildArgsFullAccess(t *testing.T) {
	cases := []struct {
		name string
		cfg  harness.Config
		spec harness.Spec
		want string
	}{
		{"off", harness.Config{}, harness.Spec{}, "auto"},
		{"on", harness.Config{FullAccess: true}, harness.Spec{}, "bypassPermissions"},
		{"on, conversation or agent mode wins", harness.Config{FullAccess: true}, harness.Spec{PermissionMode: "plan"}, "plan"},
		{"on, mode in args wins", harness.Config{FullAccess: true, Args: []string{"--permission-mode", "acceptEdits"}}, harness.Spec{}, "acceptEdits"},
		{"on, mode= form in spec args wins", harness.Config{FullAccess: true}, harness.Spec{Args: []string{"--permission-mode=plan"}}, ""},
		{"on, skip-permissions flag wins", harness.Config{FullAccess: true, Args: []string{"--dangerously-skip-permissions"}}, harness.Spec{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := buildArgs(tc.cfg, tc.spec)
			assert.Equal(t, tc.want, argAfter(args, "--permission-mode"))
			if tc.want == "" {
				assert.Equal(t, 0, countOf(args, "--permission-mode"), "onett adds no second mode: %v", args)
			}
		})
	}
}

func TestLiveSettingsUsesFullAccessDefault(t *testing.T) {
	assert.Contains(t, liveSettings(harness.Config{FullAccess: true}, harness.Spec{}), "bypassPermissions")
	assert.Contains(t, liveSettings(harness.Config{}, harness.Spec{}), `"Mode":"auto"`)
}
