import { describe, expect, it } from 'vitest';
import { agentLabel } from './agentLabel';
import { feedRows } from './activity';
import { loopLabel } from './loopLabel';
import type { OnettEvent, Problem } from '../types';

// Local-time constructors, so the expected clock times do not depend on the machine's zone.
const at = (h: number, m: number, s = 0, day = 12) => new Date(2026, 10, day, h, m, s);
const NOW = at(13, 42).getTime();

let nextId = 1;
const ev = (type: string, date: Date, extra: Partial<OnettEvent> = {}): OnettEvent => ({
  id: nextId++,
  time: date.toISOString(),
  type,
  message: '',
  ...extra,
});

// A block as the daemon writes it: "<title>: blocked: <reason>".
const blockedTask = (minute: number, id: string, cause = 'workspace setup failed') =>
  ev('task.updated', at(10, minute), {
    project: 'widgets',
    task_id: id,
    message: `Fix ${id}: blocked: ${cause}`,
    data: { id, title: `Fix ${id}`, status: 'blocked', blocked_cause: cause },
  });

const run = (minute: number, id: string, taskId: string, status: string, role = 'coder') =>
  ev('run.updated', at(10, minute), {
    project: 'widgets',
    run_id: id,
    task_id: taskId,
    data: { id, task_id: taskId, status, role },
  });

describe('feedRows, a blocked task', () => {
  it('does not make a second Blocked row when a still-blocked task is updated again', () => {
    const update = ev('task.updated', at(10, 25), {
      project: 'widgets',
      task_id: 't1',
      message: 'Fix t1: retry queued',
      data: { id: 't1', title: 'Fix t1', status: 'blocked', blocked_cause: 'workspace setup failed' },
    });
    const rows = feedRows([blockedTask(21, 't1'), update], [], NOW);
    expect(rows).toHaveLength(2);
    expect(rows.filter((r) => r.parts[0]?.text === 'Blocked: ')).toHaveLength(1);
    expect(rows[0].parts).toEqual([
      { text: 'Fix t1', to: '/tasks/t1' },
      { text: ': retry queued' },
    ]);
  });
});

describe('feedRows, folding runs', () => {
  it('folds three failed runs of one task into one row that keeps the link and counts them', () => {
    const events = [run(0, 'r1', 't1', 'failed'), run(4, 'r2', 't1', 'failed'), run(8, 'r3', 't1', 'failed')];
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ lamp: 'stop', note: '3 times · 10:00 to 10:08' });
    expect(rows[0].parts).toEqual([
      { text: `${agentLabel('coder')} run failed: ` },
      { text: 'a task', to: '/runs/r3' },
    ]);
  });

  it('folds failed runs of three different tasks into a count with no link', () => {
    const events = [run(0, 'r1', 't1', 'failed'), run(3, 'r2', 't2', 'failed'), run(6, 'r3', 't3', 'failed')];
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ lamp: 'stop', note: '10:00 to 10:06' });
    expect(rows[0].parts).toEqual([{ text: `3 ${agentLabel('coder')} runs failed` }]);
  });

  it('folds runs that started on three tasks into a count', () => {
    const events = [
      run(0, 'r1', 't1', 'starting'),
      run(2, 'r2', 't2', 'starting'),
      run(4, 'r3', 't3', 'starting'),
    ];
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0].parts).toEqual([{ text: `${agentLabel('coder')} started 3 runs` }]);
    expect(rows[0].note).toBe('10:00 to 10:04');
  });
});

describe('feedRows, folding task updates and loops', () => {
  it('folds two re-queues of different tasks with the same message into one count', () => {
    const requeue = (minute: number, id: string) =>
      ev('task.updated', at(10, minute), {
        project: 'widgets',
        task_id: id,
        message: `Fix ${id}: re-queued: workspace setup failed`,
        data: { id, title: `Fix ${id}`, status: 'queued' },
      });
    const rows = feedRows([requeue(0, 't1'), requeue(2, 't2')], [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0].parts).toEqual([{ text: '2 tasks: re-queued: workspace setup failed' }]);
    expect(rows[0].note).toBe('10:00 to 10:02');
  });

  it('shows a loop error as the loop name and failed, with the streak note and no error text', () => {
    const problems: Problem[] = [
      {
        kind: 'failing_loops',
        project: 'widgets',
        key: 'failing_loops:widgets',
        title: '',
        detail: '',
        count: 13,
        since: at(10, 24).toISOString(),
        loops: [{ name: 'issue_intake', fails: 13, since: at(10, 24).toISOString() }],
      },
    ];
    const events = [
      ev('loop.error', at(13, 30), { project: 'widgets', data: { loop: 'issue_intake', error: 'secret boom text' } }),
    ];
    const [row] = feedRows(events, problems, NOW);
    expect(row.lamp).toBe('stop');
    expect(row.parts).toEqual([{ text: loopLabel('issue_intake'), to: '/loops' }, { text: ' failed' }]);
    expect(row.note).toBe('13 times since 10:24');
    expect(JSON.stringify(row)).not.toContain('secret boom text');
  });
});

describe('feedRows, notifications', () => {
  it('makes no row for a notification that says a task is Blocked', () => {
    const events = [ev('notification', at(10, 21), { project: 'widgets', message: 'Blocked: Publish agent screenshots' })];
    expect(feedRows(events, [], NOW)).toEqual([]);
  });

  it('names the kind and links the entity for a Needs you notification, and takes its project', () => {
    const events = [
      ev('notification', at(12, 0), { message: 'Needs you: blocked_task 01AB (widgets)' }),
    ];
    const [row] = feedRows(events, [], NOW);
    expect(row.project).toBe('widgets');
    expect(row.parts).toEqual([
      { text: 'Told you: ' },
      { text: 'blocked task', to: '/tasks/01AB' },
      { text: ' on widgets' },
    ]);
  });

  it('gives no link for a Needs you notification with a kind it does not know', () => {
    const events = [ev('notification', at(12, 0), { message: 'Needs you: mystery_kind x (p)' })];
    const [row] = feedRows(events, [], NOW);
    expect(row.parts).toEqual([{ text: 'Told you: ' }, { text: 'mystery kind' }, { text: ' on p' }]);
  });

  it('keeps the plain sentence for a notification with other text', () => {
    const events = [ev('notification', at(12, 0), { project: 'widgets', message: 'Deploy paused for review' })];
    const [row] = feedRows(events, [], NOW);
    expect(row.parts).toEqual([{ text: 'Told you: Deploy paused for review' }]);
    expect(row.project).toBe('widgets');
  });
});
