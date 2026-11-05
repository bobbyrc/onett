import { describe, expect, it } from 'vitest';
import { describePause } from './pause';

const since = '2026-11-04T22:36:00';

describe('describePause', () => {
  it('names the commit and the run a self-update waits for', () => {
    const text = describePause({
      reason: 'self-update',
      by: 'api token',
      detail: '69f1660',
      since,
      waiting: [{ run_id: 'r1', task_id: '01a1a9c6-aaaa-bbbb' }],
    });
    expect(text).toMatch(/^Paused for self-update to 69f1660 since \d\d:\d\d, waiting for 1 run \(task 01a1a9c6…\)\.$/);
  });

  it('counts several runs', () => {
    const waiting = ['a', 'b', 'c', 'd'].map((x) => ({ run_id: x, task_id: x }));
    expect(describePause({ reason: 'self-update', by: '', detail: '', since, waiting })).toContain('waiting for 4 runs (tasks a, b, c, …)');
  });

  it('says who paused for an owner pause', () => {
    expect(describePause({ reason: 'owner', by: 'bob', detail: '', since, waiting: [] })).toMatch(/^Paused by bob since \d\d:\d\d\.$/);
  });
});
