package codex

import "github.com/bobbyrc/onett/internal/harness"

// Permission mode values for codex. The first is the default onett uses; the sandbox names are
// codex's own --sandbox values.
const (
	permApproveForMe = "approve-for-me"
	permBypass       = "dangerously-bypass-approvals-and-sandbox"
)

// This is the one place the choices offered to the owner are listed. Codex has no model aliases:
// the models named in the agents config are added by the engine.
func init() {
	harness.RegisterChoices(harnessType, harness.Choices{
		Efforts: []harness.Choice{
			{Value: "minimal", Label: "Minimal"},
			{Value: "low", Label: "Low"},
			{Value: "medium", Label: "Medium"},
			{Value: "high", Label: "High"},
			{Value: "xhigh", Label: "Extra high"},
		},
		PermissionModes: []harness.Choice{
			{Value: permApproveForMe, Label: "Approve for me", Hint: "Workspace-write sandbox; approvals reviewed automatically"},
			{Value: "read-only", Label: "Read-only", Hint: "The agent can read but not change files"},
			{Value: "workspace-write", Label: "Workspace write", Hint: "The agent can write inside the workspace"},
			{Value: "danger-full-access", Label: "Full access", Hint: "No sandbox", Dangerous: true},
			{Value: permBypass, Label: "Bypass approvals and sandbox", Hint: "Skips every safety check", Dangerous: true},
		},
	})
}
