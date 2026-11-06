// The last deploy line on an Overview board window: each project's newest deployment, in words.
import type { DeployState, Deployment } from '../types';
import { parseTime } from './time';

const WORDS: Record<DeployState, string> = {
  pending: 'Deploy pending',
  deploying: 'Deploying',
  verifying: 'Verifying deploy',
  healthy: 'Deployed',
  failed: 'Deploy failed',
  rolled_back: 'Rolled back',
};

/** "Deployed", "Rolled back": what the deploy did, so the state never rests on colour. */
export function deployWords(state: DeployState): string {
  return WORDS[state] ?? `Deploy ${String(state).replace(/_/g, ' ')}`;
}

/** A deploy that ended badly: its words take the board's stop colour. */
export function deployStopped(state: DeployState): boolean {
  return state === 'failed' || state === 'rolled_back';
}

/** The newest deployment of each project, by start time, whatever order the list came in. */
export function latestDeploys(deployments: readonly Deployment[]): Map<string, Deployment> {
  const out = new Map<string, Deployment>();
  const started = (d: Deployment) => parseTime(d.started_at)?.getTime() ?? 0;
  for (const d of deployments) {
    const seen = out.get(d.project);
    if (!seen || started(d) > started(seen)) out.set(d.project, d);
  }
  return out;
}
