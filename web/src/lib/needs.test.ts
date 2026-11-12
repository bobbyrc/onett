import { describe, expect, it } from 'vitest';
import type { Approval, Problem } from '../types';
import { ageWords, buildNeeds, needsByProject } from './needs';

const problem = (over: Partial<Problem>): Problem => ({
  kind: 'blocked_tasks',
  project: 'app',
  key: 'k',
  title: 't',
  detail: '',
  count: 1,
  since: '2026-11-12T10:00:00Z',
  ...over,
});

const approval = (over: Partial<Approval>): Approval => ({
  id: 'a1',
  task_id: 't1',
  project: 'app',
  kind: 'merge',
  title: 'Add login',
  reason: '',
  status: 'pending',
  created_at: '2026-11-12T10:00:00Z',
  ...over,
});

describe('buildNeeds', () => {
  it('puts a seven-task block before a one-task block, loops, a goal and then approvals by age', () => {
    const needs = buildNeeds(
      [
        problem({ key: 'goal', kind: 'blocked_goal', since: '2026-11-01T00:00:00Z' }),
        problem({ key: 'loops', kind: 'failing_loops', since: '2026-11-01T00:00:00Z' }),
        problem({ key: 'one', count: 1, since: '2026-11-01T00:00:00Z' }),
        problem({ key: 'seven', count: 7, since: '2026-11-12T10:00:00Z' }),
      ],
      [
        approval({ id: 'new', created_at: '2026-11-12T11:00:00Z' }),
        approval({ id: 'old', created_at: '2026-11-12T09:00:00Z' }),
      ],
    );
    expect(needs.map((n) => n.key)).toEqual(['seven', 'one', 'loops', 'goal', 'approval:old', 'approval:new']);
  });

  it('leaves out decided approvals', () => {
    const needs = buildNeeds([], [approval({ id: 'a', status: 'approved' }), approval({ id: 'b', status: 'rejected' })]);
    expect(needs).toEqual([]);
  });

  it('does not change its arguments', () => {
    const problems = [problem({ key: 'b', count: 1 }), problem({ key: 'a', count: 7 })];
    const approvals = [approval({ id: 'x' })];
    const before = JSON.stringify([problems, approvals]);
    buildNeeds(problems, approvals);
    expect(JSON.stringify([problems, approvals])).toBe(before);
  });
});

describe('needsByProject', () => {
  it('groups by project, keeping the order inside each group and the order projects first appear', () => {
    const needs = buildNeeds(
      [
        problem({ key: 'a1', project: 'alpha', count: 3 }),
        problem({ key: 'b1', project: 'beta', count: 2 }),
        problem({ key: 'a2', project: 'alpha', count: 1 }),
      ],
      [],
    );
    const grouped = needsByProject(needs);
    expect([...grouped.keys()]).toEqual(['alpha', 'beta']);
    expect(grouped.get('alpha')?.map((n) => n.key)).toEqual(['a1', 'a2']);
  });
});

describe('ageWords', () => {
  const minute = 60_000;
  const hour = 60 * minute;
  it('gives a coarse age with no "ago"', () => {
    expect(ageWords(30_000)).toBe('under a minute');
    expect(ageWords(-5_000)).toBe('under a minute');
    expect(ageWords(59 * minute)).toBe('59m');
    expect(ageWords(60 * minute)).toBe('1h');
    expect(ageWords(47 * hour)).toBe('47h');
    expect(ageWords(48 * hour)).toBe('2d');
    expect(ageWords(9 * 24 * hour)).toBe('9d');
  });
});
