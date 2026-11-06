import type { Run } from '../types';

// Where a task-less (chat) run's label points: its conversation in Chat when the run records one,
// otherwise the run's own page.
export function chatRunHref(run: Pick<Run, 'id' | 'conversation_id'>): string {
  return run.conversation_id ? `/chat/${run.conversation_id}` : `/runs/${run.id}`;
}
