import { describe, expect, it } from 'vitest';
import type { Task } from '../types';
import { groupOf, groupTasks, queueLabel } from './taskGroups';

const t = (id: string, status: Task['status'], extra: Partial<Task> = {}): Task =>
  ({ id, status, ...extra }) as Task;

describe('groupTasks', () => {
  it('puts each status in its group and keeps the server order inside a group', () => {
    const groups = groupTasks([
      t('run', 'running'),
      t('rev', 'review'),
      t('dep', 'deploying'),
      t('q1', 'queued'),
      t('q2', 'queued'),
      t('w', 'queued', { waiting_on: ['run'] }),
      t('b', 'blocked'),
      t('d', 'done'),
      t('f', 'failed'),
    ]);
    expect(groups.map((g) => [g.key, g.tasks.map((x) => x.id)])).toEqual([
      ['running', ['run']],
      ['in_progress', ['rev', 'dep']],
      ['queued', ['q1', 'q2']],
      ['waiting', ['w']],
      ['blocked', ['b']],
      ['finished', ['d', 'f']],
    ]);
  });

  it('leaves out empty groups', () => {
    expect(groupTasks([t('q', 'queued')]).map((g) => g.key)).toEqual(['queued']);
    expect(groupTasks([])).toEqual([]);
  });

  it('treats a non-queued task with waiting_on as not waiting', () => {
    expect(groupOf(t('x', 'review', { waiting_on: ['y'] }))).toBe('in_progress');
  });
});

describe('queueLabel', () => {
  it('marks the first as next and shows the project position when it differs', () => {
    expect(queueLabel({ queue_position: 1, project_queue_position: 1 })).toBe('#1 next');
    expect(queueLabel({ queue_position: 3, project_queue_position: 3 })).toBe('#3');
    expect(queueLabel({ queue_position: 3, project_queue_position: 1 })).toBe('#3 (#1 in project)');
    expect(queueLabel({})).toBe('');
  });
});
