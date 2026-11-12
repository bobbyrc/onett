import { describe, expect, it } from 'vitest';
import { keysFor } from './events';
import { keys } from './keys';
import type { OnettEvent } from './types';

const ev = (type: string, extra: Partial<OnettEvent> = {}): OnettEvent => ({
  id: 1,
  time: '2026-11-12T12:00:00Z',
  type,
  message: '',
  ...extra,
});

describe('keysFor problems', () => {
  it('refreshes problems when a task, goal or loop changes and when the owner is told', () => {
    expect(keysFor(ev('task.updated', { task_id: 't1' }))).toContainEqual(keys.problemsAll);
    expect(keysFor(ev('goal.updated', { data: { id: 'g1' } }))).toContainEqual(keys.problemsAll);
    expect(keysFor(ev('loop.error', { data: { loop: 'nightly', error: 'x' } }))).toContainEqual(keys.problemsAll);
    expect(keysFor(ev('notification', { message: 'Told you' }))).toContainEqual(keys.problemsAll);
  });

  it('does not refresh problems for a new task or a run', () => {
    expect(keysFor(ev('task.created', { task_id: 't1' }))).not.toContainEqual(keys.problemsAll);
    expect(keysFor(ev('run.updated', { run_id: 'r1', task_id: 't1' }))).not.toContainEqual(keys.problemsAll);
  });
});
