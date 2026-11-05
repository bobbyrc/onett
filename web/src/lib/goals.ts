// Which controls a goal offers. They mirror the server's rules (docs/api.md): pause needs an
// active goal, resume a paused or blocked one, block an active or paused one, done any not done.
import type { GoalStatus } from '../types';

export type GoalAction = 'pause' | 'resume' | 'done' | 'block';

export function goalActions(status: GoalStatus): GoalAction[] {
  switch (status) {
    case 'active':
      return ['pause', 'block', 'done'];
    case 'paused':
      return ['resume', 'block', 'done'];
    case 'blocked':
      return ['resume', 'done'];
    default:
      return [];
  }
}

/** Done and block need a written reason; the server rejects an empty one. */
export function needsText(action: GoalAction): boolean {
  return action === 'done' || action === 'block';
}
