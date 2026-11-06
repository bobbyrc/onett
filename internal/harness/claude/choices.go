package claude

import "github.com/bobbyrc/onett/internal/harness"

// Claude Code accepts --model <alias|name>, --effort and --permission-mode. This is the one place
// the choices offered to the owner are listed.
func init() {
	harness.RegisterChoices(harnessType, harness.Choices{
		Models: []harness.Choice{
			{Value: "opus", Label: "Opus"},
			{Value: "sonnet", Label: "Sonnet"},
			{Value: "haiku", Label: "Haiku"},
		},
		Efforts: []harness.Choice{
			{Value: "low", Label: "Low"},
			{Value: "medium", Label: "Medium"},
			{Value: "high", Label: "High"},
			{Value: "xhigh", Label: "Extra high"},
			{Value: "max", Label: "Max"},
		},
		PermissionModes: []harness.Choice{
			{Value: "auto", Label: "Auto", Hint: "A classifier reviews risky actions"},
			{Value: "acceptEdits", Label: "Accept edits", Hint: "File edits are approved; other actions are checked"},
			{Value: "plan", Label: "Plan", Hint: "Read-only: the agent plans and does not change files"},
			{Value: "dontAsk", Label: "Don't ask", Hint: "Anything not pre-approved is refused instead of asked"},
			{Value: "manual", Label: "Manual", Hint: "Every action needs approval, so unattended turns stall"},
			{Value: "bypassPermissions", Label: "Bypass permissions", Hint: "Skips every permission check", Dangerous: true},
		},
	})
}
