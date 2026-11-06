// Words for the Loops page: a loop's readable name and its state as a word beside the lamp.
import type { LoopView } from '../types';
import { humanizeAgentName } from './agentLabel';
import { loopHealth } from './projects';
import { parseTime } from './time';

// Loop ids whose words do not title-case cleanly.
const LABELS: Record<string, string> = {
  pr_shepherd: 'PR Shepherd',
};

/** A loop id ("issue_intake") as Title Case words ("Issue Intake"). Only a label: the API and config keep the id. */
export function loopLabel(name: string): string {
  return LABELS[name] ?? humanizeAgentName(name);
}

/**
 * The word shown beside a loop's lamp. It tells apart the two grey states loopHealth merges
 * (off, and on but never run), so the word says more than the colour.
 */
export function loopStatusWord(loop: LoopView): string {
  if (!loop.enabled) return 'Off';
  if (loopHealth(loop) === 'danger') return 'Failed';
  if (!parseTime(loop.last_run_at)) return 'Not run yet';
  return 'OK';
}
