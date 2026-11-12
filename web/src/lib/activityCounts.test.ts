import { describe, expect, it } from 'vitest';
import { feedRows } from './activity';
import type { OnettEvent } from '../types';

// Local-time constructor, so the expected clock times do not depend on the machine's zone.
const at = (h: number, m: number, s = 0, day = 12) => new Date(2026, 10, day, h, m, s);
const NOW = at(13, 42).getTime();

let nextId = 1000;
const ev = (type: string, date: Date, extra: Partial<OnettEvent> = {}): OnettEvent => ({
  id: nextId++,
  time: date.toISOString(),
  type,
  message: '',
  ...extra,
});

const blocked = (minute: number, id: string, cause = 'workspace setup failed') =>
  ev('task.updated', at(10, minute), {
    project: 'widgets',
    task_id: id,
    message: `Fix ${id}: blocked: ${cause}`,
    data: { id, title: `Fix ${id}`, status: 'blocked', blocked_cause: cause },
  });

const requeued = (minute: number, id: string) =>
  ev('task.updated', at(10, minute), {
    project: 'widgets',
    task_id: id,
    message: `Fix ${id}: re-queued: workspace setup failed`,
    data: { id, title: `Fix ${id}`, status: 'queued', blocked_cause: 'workspace setup failed' },
  });

const failedRun = (minute: number, id: string, taskId: string) =>
  ev('run.updated', at(10, minute), {
    project: 'widgets',
    run_id: id,
    task_id: taskId,
    data: { id, task_id: taskId, status: 'failed', role: 'senior-engineer' },
  });

const notice = (minute: number, project: string, message: string) =>
  ev('notification', at(10, minute), { project, message });

describe('feedRows counts distinct tasks, not events', () => {
  it('counts seven tasks blocked when one of them is blocked twice', () => {
    const events = [0, 1, 2, 3, 4, 5, 6].map((i) => blocked(i, `t${i}`));
    events.push(blocked(8, 't0'));
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0].parts).toEqual([{ text: '7 tasks blocked: workspace setup failed' }]);
  });

  it('counts three tasks when each is re-queued twice', () => {
    const events = [0, 1, 2].map((i) => requeued(i, `t${i}`));
    events.push(...[0, 1, 2].map((i) => requeued(5 + i, `t${i}`)));
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0].parts).toEqual([{ text: '3 tasks: re-queued: workspace setup failed' }]);
  });

  it('counts runs for a run fold, since the same runs are a count of events', () => {
    const events = [0, 1, 2].flatMap((i) => [failedRun(i, `r${i}a`, 't1'), failedRun(i + 3, `r${i}b`, 't2')]);
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0].parts).toEqual([{ text: '6 Senior Engineer runs failed' }]);
  });
});

describe('feedRows keeps notifications apart by project', () => {
  it('keeps two notifications for different projects as two rows', () => {
    const rows = feedRows(
      [notice(0, 'widgets', 'Needs you: blocked_task t1 (widgets)'), notice(0, 'gadgets', 'Needs you: blocked_task t2 (gadgets)')],
      [],
      NOW,
    );
    expect(rows).toHaveLength(2);
  });

  it('folds two notifications for the same project into one count', () => {
    const rows = feedRows(
      [notice(0, 'widgets', 'Needs you: blocked_task t1 (widgets)'), notice(1, 'widgets', 'Needs you: blocked_task t2 (widgets)')],
      [],
      NOW,
    );
    expect(rows).toHaveLength(1);
    expect(rows[0].parts[0].text).toMatch(/^Told you 2 times, latest: /);
  });
});
