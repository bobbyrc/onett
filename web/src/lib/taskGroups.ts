import type { Task } from '../types';

export type TaskGroupKey = 'running' | 'in_progress' | 'queued' | 'waiting' | 'blocked' | 'finished';

export interface TaskGroup {
  key: TaskGroupKey;
  label: string;
  tasks: Task[];
}

const LABELS: Record<TaskGroupKey, string> = {
  running: 'Running now',
  in_progress: 'In review, CI or deploy',
  queued: 'Queued, in the order they will start',
  waiting: 'Waiting on other tasks',
  blocked: 'Blocked',
  finished: 'Finished',
};

/** Which group a task belongs to. Mirrors the server's pickup order (store.GroupOf). */
export function groupOf(t: Pick<Task, 'status' | 'waiting_on'>): TaskGroupKey {
  switch (t.status) {
    case 'running':
      return 'running';
    case 'review':
    case 'awaiting_approval':
    case 'merged':
    case 'deploying':
      return 'in_progress';
    case 'queued':
      return (t.waiting_on?.length ?? 0) > 0 ? 'waiting' : 'queued';
    case 'blocked':
      return 'blocked';
    default:
      return 'finished';
  }
}

/**
 * Splits tasks, already in the server's pickup order, into their groups. The order inside a group
 * is kept, and empty groups are left out.
 */
export function groupTasks(tasks: Task[]): TaskGroup[] {
  const groups = new Map<TaskGroupKey, Task[]>();
  for (const t of tasks) {
    const key = groupOf(t);
    groups.set(key, [...(groups.get(key) ?? []), t]);
  }
  return (Object.keys(LABELS) as TaskGroupKey[])
    .filter((key) => groups.has(key))
    .map((key) => ({ key, label: LABELS[key], tasks: groups.get(key) ?? [] }));
}

/** The queue badge: "#1 next", "#2", plus the project's own count when it differs. */
export function queueLabel(t: Pick<Task, 'queue_position' | 'project_queue_position'>): string {
  const pos = t.queue_position ?? 0;
  if (pos === 0) return '';
  let out = pos === 1 ? '#1 next' : `#${pos}`;
  if (t.project_queue_position && t.project_queue_position !== pos) out += ` (#${t.project_queue_position} in project)`;
  return out;
}
