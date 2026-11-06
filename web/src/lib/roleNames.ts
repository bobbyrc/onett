// The built-in agents were renamed to job roles. Tasks and runs stored before the rename still
// carry the old names, so every place that shows a role passes it through here. Keep in step with
// legacyAgentNames in internal/config/agent_names.go.
const legacyRoleNames: Record<string, string> = {
  orchestrator: 'chief-of-staff',
  implementer: 'senior-engineer',
  reviewer: 'code-reviewer',
  triager: 'tech-lead',
  improver: 'platform-engineer',
};

/** The current name for a stored role: the job-role name for an old built-in name, else unchanged. */
export function roleName(role: string): string {
  return Object.hasOwn(legacyRoleNames, role) ? legacyRoleNames[role] : role;
}
