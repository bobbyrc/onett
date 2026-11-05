import type { AgentView, ProjectView, SecretView, Stats } from '../types';

export interface SetupStep {
  id: 'project' | 'agents' | 'policy' | 'reach' | 'secrets';
  label: string;
  /** What is left to do, or what was found. */
  detail: string;
  done: boolean;
  to: string;
}

/** The data the checklist reads. `undefined` means not loaded yet (or not allowed): the step is left out. */
export interface SetupInputs {
  projects?: Pick<ProjectView, 'name' | 'policy'>[];
  agents?: Pick<AgentView, 'name' | 'disabled' | 'harness_cli_found'>[];
  stats?: Pick<Stats, 'owner_unreachable'>;
  secrets?: Pick<SecretView, 'name' | 'set' | 'referenced_by'>[];
}

/**
 * The first-run steps. A step whose data has not loaded is left out, so the card never claims
 * "all done" (or "not done") from missing information. Secrets are owner-only: pass `undefined`
 * for anyone else and that step is skipped.
 */
export function setupSteps(in_: SetupInputs): SetupStep[] {
  const steps: SetupStep[] = [];
  const { projects, agents, stats, secrets } = in_;

  if (projects) {
    steps.push({
      id: 'project',
      label: 'Add a project',
      detail: projects.length > 0 ? `${projects.length} configured` : 'No project yet: tell onett which repository to work on.',
      done: projects.length > 0,
      to: '/projects',
    });
  }
  if (agents) {
    const enabled = agents.filter((a) => !a.disabled);
    const ready = enabled.filter((a) => a.harness_cli_found).length;
    const missing = enabled.length - ready;
    const noun = `agent${ready === 1 ? '' : 's'}`;
    let detail = `${ready} ${noun} ready`;
    if (enabled.length === 0) detail = 'No agent is enabled.';
    else if (ready === 0) detail = 'No enabled agent has its CLI installed: run onett doctor.';
    else if (missing > 0) detail += `; ${missing} enabled with no CLI found`;
    steps.push({
      id: 'agents',
      label: 'Agents ready',
      detail,
      done: ready > 0,
      to: '/config?section=agents',
    });
  }
  if (projects) {
    // A project with no policy of its own uses the default, so any project resolves one.
    steps.push({
      id: 'policy',
      label: 'Choose a policy',
      detail: projects.length > 0 ? 'Every project has a policy (the default counts)' : 'Add a project first; the default policy applies to it.',
      done: projects.length > 0,
      to: '/config?section=policies',
    });
  }
  if (stats) {
    steps.push({
      id: 'reach',
      label: 'Reach me',
      detail: stats.owner_unreachable ? 'Set up a notifier or channel (see the warning on this page).' : 'A notifier or channel can reach you',
      done: !stats.owner_unreachable,
      to: '/config?mode=setup',
    });
  }
  if (secrets) {
    const missing = secrets.filter((s) => !s.set && s.referenced_by.length > 0);
    steps.push({
      id: 'secrets',
      label: 'Secrets set',
      detail: missing.length > 0 ? `Not set: ${missing.map((s) => s.name).join(', ')}` : 'Every secret the config refers to is set',
      done: missing.length === 0,
      to: '/config?mode=secrets',
    });
  }
  return steps;
}

/** True when there is something to show: some step loaded and not every loaded step is done. */
export function setupIncomplete(steps: SetupStep[]): boolean {
  return steps.length > 0 && steps.some((s) => !s.done);
}
