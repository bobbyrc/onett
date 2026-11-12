import { describe, expect, it } from 'vitest';
import type { Approval, Problem } from '../types';
import { buildNeeds } from './needs';
import { verdictFor } from './verdict';

const NOW = Date.parse('2026-11-12T12:00:00Z');
const ago = (ms: number) => new Date(NOW - ms).toISOString();
const HOUR = 60 * 60_000;
const base = { owner: true, activeRuns: 0, paused: false, now: NOW };

const problem = (over: Partial<Problem>): Problem => ({
  kind: 'blocked_tasks',
  project: 'app',
  key: 'k',
  title: 'Something failed',
  detail: '',
  count: 1,
  since: ago(2 * HOUR),
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
  created_at: ago(2 * HOUR),
  ...over,
});

describe('verdictFor when the list did not load', () => {
  it('does not say nothing needs the owner', () => {
    const v = verdictFor({ ...base, projects: 2, needs: [], failed: true });
    expect(v).toEqual({
      lamp: 'off',
      head: 'Cannot tell what needs you',
      lead: '',
      rest: 'The list did not load. The error is shown below.',
    });
  });

  it('still says not set up yet when there are no projects', () => {
    expect(verdictFor({ ...base, projects: 0, needs: [], failed: true }).head).toBe('Not set up yet');
  });
});

describe('verdictFor cuts long titles', () => {
  it('keeps the first 99 characters and an ellipsis, with no full stop after it', () => {
    const long = 'word '.repeat(30).trim();
    const needs = buildNeeds([problem({ title: long })], [approval({ id: 'a1' })]);
    const v = verdictFor({ ...base, projects: 2, needs, failed: false });
    expect(v.rest).toContain(`${long.slice(0, 99)}… Start there.`);
    expect(v.rest).not.toContain('….');
  });
});
