import { describe, expect, it } from 'vitest';
import { agentLabel } from './agentLabel';
import { feedRows, shortTime } from './activity';
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

const blockedTask = (minute: number, id: string, cause = 'workspace setup failed') =>
  ev('task.updated', at(10, minute), {
    project: 'widgets',
    task_id: id,
    message: `Fix ${id}: blocked: ${cause}`,
    data: { id, title: `Fix ${id}`, status: 'blocked', blocked_cause: cause },
  });

describe('feedRows, plumbing and quiet events', () => {
  it('gives no row for plumbing event types', () => {
    const events = ['run.log', 'chat.delta', 'chat.message', 'conversation.updated', 'trigger.updated'].map((t) =>
      ev(t, at(10, 0), { data: {} }),
    );
    expect(feedRows(events, [], NOW)).toEqual([]);
  });

  it('gives no row for a chat turn that runs and succeeds', () => {
    const events = [
      ev('run.updated', at(10, 0), { run_id: 'r1', data: { id: 'r1', status: 'running', role: 'orchestrator' } }),
      ev('run.updated', at(10, 1), { run_id: 'r1', data: { id: 'r1', status: 'succeeded', role: 'orchestrator' } }),
    ];
    expect(feedRows(events, [], NOW)).toEqual([]);
  });
});

describe('feedRows, task runs', () => {
  it('names the task from an earlier task.created when a run starts', () => {
    const events = [
      ev('task.created', at(10, 0), { task_id: 't1', data: { id: 't1', title: 'Fix login' } }),
      ev('run.updated', at(10, 5), { run_id: 'r2', task_id: 't1', data: { id: 'r2', task_id: 't1', status: 'starting', role: 'coder' } }),
    ];
    const rows = feedRows(events, [], NOW);
    expect(rows[0].parts).toEqual([
      { text: `${agentLabel('coder')} started on ` },
      { text: 'Fix login', to: '/runs/r2' },
    ]);
  });

  it('gives a failed run the stop lamp', () => {
    const events = [
      ev('task.updated', at(10, 0), { task_id: 't1', data: { id: 't1', title: 'Fix login', status: 'running' } }),
      ev('run.updated', at(10, 5), { run_id: 'r3', task_id: 't1', data: { id: 'r3', task_id: 't1', status: 'failed', role: 'reviewer' } }),
    ];
    const [row] = feedRows(events, [], NOW);
    expect(row.lamp).toBe('stop');
    expect(row.parts).toEqual([
      { text: `${agentLabel('reviewer')} run failed: ` },
      { text: 'Fix login', to: '/runs/r3' },
    ]);
  });
});

describe('feedRows, folding repeats', () => {
  it('folds seven blocked tasks with one cause and seven notifications into two rows', () => {
    const events: OnettEvent[] = [];
    for (let i = 0; i < 7; i++) {
      events.push(blockedTask(21 + i, `t${i + 1}`));
      events.push(ev('notification', at(10, 21 + i, 30), { project: 'widgets', message: `Notice ${i + 1}` }));
    }
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({
      lamp: undefined,
      parts: [{ text: 'Told you 7 times, latest: Notice 7' }],
      note: '10:21 to 10:27',
      project: 'widgets',
    });
    expect(rows[1]).toMatchObject({
      lamp: 'stop',
      parts: [{ text: '7 tasks blocked: workspace setup failed' }],
      note: '10:21 to 10:27',
      project: 'widgets',
    });
  });

  it('does not fold two blocked tasks eleven minutes apart', () => {
    const rows = feedRows([blockedTask(0, 't1'), blockedTask(11, 't2')], [], NOW);
    expect(rows).toHaveLength(2);
    expect(rows.every((r) => r.note === undefined)).toBe(true);
    expect(rows[0].parts).toEqual([
      { text: 'Blocked: ' },
      { text: 'Fix t2', to: '/tasks/t2' },
    ]);
  });
});

describe('feedRows, loops, approvals and fallbacks', () => {
  it('names a failing loop without its error text and says how often it failed', () => {
    const error = 'a'.repeat(300);
    const problems: Problem[] = [
      {
        kind: 'failing_loops',
        project: 'widgets',
        key: 'failing_loops:widgets',
        title: '',
        detail: '',
        count: 13,
        since: at(10, 24).toISOString(),
        loops: [{ name: 'nightly', fails: 13, since: at(10, 24).toISOString() }],
      },
    ];
    const events = [ev('loop.error', at(13, 30), { project: 'widgets', data: { loop: 'nightly', error } })];
    const [row] = feedRows(events, problems, NOW);
    expect(row.lamp).toBe('stop');
    expect(row.parts).toEqual([
      { text: loopLabel('nightly'), to: '/loops' },
      { text: ' failed' },
    ]);
    expect(row.note).toBe('13 times since 10:24');
  });

  it('gives an approval request the call lamp and links to the task', () => {
    const events = [
      ev('approval.requested', at(12, 0), {
        project: 'widgets',
        data: { kind: 'merge', title: 'Add login', task_id: 't1' },
      }),
    ];
    const [row] = feedRows(events, [], NOW);
    expect(row).toMatchObject({ lamp: 'call', project: 'widgets' });
    expect(row.parts).toEqual([
      { text: 'Merge approval asked: ' },
      { text: 'Add login', to: '/tasks/t1' },
    ]);
  });

  it('falls back to the message and a task link for an unknown type', () => {
    const events = [ev('something.new', at(12, 0), { task_id: 't1', message: 'hello world' })];
    const [row] = feedRows(events, [], NOW);
    expect(row.parts).toEqual([{ text: 'Hello world' }, { text: ' ' }, { text: 'task', to: '/tasks/t1' }]);
  });
});

describe('shortTime', () => {
  it('gives the clock time for today', () => {
    expect(shortTime(at(9, 5), NOW)).toBe('09:05');
  });

  it('gives the weekday and time within the six days before', () => {
    // 9 November 2026 is a Monday.
    expect(shortTime(at(9, 5, 0, 9), NOW)).toBe('Mon 09:05');
  });

  it('gives the day and month further back', () => {
    expect(shortTime(new Date(2026, 9, 22, 9, 5), NOW)).toBe('22 Oct');
  });
});
