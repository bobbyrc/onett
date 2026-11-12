import { describe, expect, it } from 'vitest';
import type { Approval, Problem } from '../types';
import { buildNeeds, type Need } from './needs';
import { verdictFor, type VerdictInput } from './verdict';

const NOW = Date.parse('2026-11-12T12:00:00Z');
const ago = (ms: number) => new Date(NOW - ms).toISOString();
const HOUR = 60 * 60_000;

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

const input = (over: Partial<VerdictInput> & { needs?: Need[] } = {}): VerdictInput => ({
  projects: 2,
  owner: true,
  needs: [],
  activeRuns: 0,
  paused: false,
  now: NOW,
  failed: false,
  ...over,
});

const one = (p: Partial<Problem>) => buildNeeds([problem(p)], []);
const oneApproval = (a: Partial<Approval>) => buildNeeds([], [approval(a)]);

describe('verdictFor, before any need', () => {
  it('says loading while the projects are unknown', () => {
    expect(verdictFor(input({ projects: undefined }))).toEqual({ lamp: 'off', head: 'Loading…', lead: '', rest: '' });
  });

  it('tells an owner with no project to start with step 1', () => {
    const v = verdictFor(input({ projects: 0, owner: true }));
    expect(v.lamp).toBe('off');
    expect(v.head).toBe('Not set up yet');
    expect(v.lead).toBe('');
    expect(v.rest).toBe(
      'onett has no project to work on. Start with step 1 below. Each project you add gets a row here: what needs you on the left, what is running on the right.',
    );
  });

  it('tells anyone else with no project to ask an owner', () => {
    const v = verdictFor(input({ projects: 0, owner: false }));
    expect(v.rest).toBe('onett has no project to work on. An owner can add one on the Projects page.');
  });

  it('says nothing needs you and the line is paused', () => {
    expect(verdictFor(input({ paused: true, activeRuns: 2 }))).toEqual({
      lamp: 'off',
      head: 'Nothing needs you',
      lead: '',
      rest: 'The line is paused: no new runs start.',
    });
  });

  it('counts active runs when nothing needs you', () => {
    expect(verdictFor(input({ activeRuns: 1 })).rest).toBe('1 run is active.');
    expect(verdictFor(input({ activeRuns: 3 })).rest).toBe('3 runs are active.');
    expect(verdictFor(input({ activeRuns: 3 })).lamp).toBe('run');
  });

  it('says nothing is running when nothing needs you and nothing runs', () => {
    expect(verdictFor(input())).toEqual({ lamp: 'off', head: 'Nothing needs you', lead: '', rest: 'Nothing is running.' });
  });
});

describe('verdictFor, one row of the table per kind of top need', () => {
  it('a block of several tasks stops the work and names the cause', () => {
    const v = verdictFor(input({ needs: one({ count: 7, title: 'workspace setup failed', project: 'widgets' }) }));
    expect(v).toEqual({
      lamp: 'stop',
      head: 'Work is stopped on widgets',
      lead: '7 tasks on widgets are blocked:',
      rest: 'workspace setup failed.',
    });
  });

  it('a block of several tasks with four more needs says where to start', () => {
    const needs = buildNeeds(
      [problem({ count: 7, title: 'workspace setup failed', project: 'widgets' })],
      [approval({ id: 'a1' }), approval({ id: 'a2' }), approval({ id: 'a3' }), approval({ id: 'a4' })],
    );
    const v = verdictFor(input({ needs }));
    expect(v.head).toBe('Work is stopped on widgets');
    expect(v.lead).toBe('7 tasks on widgets are blocked:');
    expect(v.rest).toBe('workspace setup failed. Start there. 4 more things follow.');
  });

  it('one blocked task names its title and the age of the block', () => {
    const v = verdictFor(
      input({ needs: one({ count: 1, title: 'Fix login', tasks: [{ id: 't1', title: 'Fix login', attempts: 1, max_attempts: 3 }] }) }),
    );
    expect(v).toEqual({
      lamp: 'stop',
      head: '1 thing needs you',
      lead: 'A task on app has been blocked for 2h:',
      rest: 'Fix login.',
    });
  });

  it('one blocked task with no task list uses the problem title', () => {
    const v = verdictFor(input({ needs: one({ count: 1, title: 'Disk full' }) }));
    expect(v.rest).toBe('Disk full.');
  });

  it('one failing loop', () => {
    const v = verdictFor(
      input({
        needs: one({
          kind: 'failing_loops',
          title: '',
          count: 1,
          since: ago(3 * HOUR),
          loops: [{ name: 'nightly', fails: 1, since: ago(3 * HOUR) }],
        }),
      }),
    );
    expect(v).toEqual({
      lamp: 'stop',
      head: '1 thing needs you',
      lead: '1 loop on app has been failing for 3h.',
      rest: '',
    });
  });

  it('several failing loops count the loops listed', () => {
    const v = verdictFor(
      input({
        needs: one({
          kind: 'failing_loops',
          title: '',
          count: 2,
          since: ago(3 * HOUR),
          loops: [
            { name: 'nightly', fails: 2, since: ago(3 * HOUR) },
            { name: 'sweep', fails: 1, since: ago(HOUR) },
          ],
        }),
      }),
    );
    expect(v.lead).toBe('2 loops on app have been failing for 3h.');
    expect(v.rest).toBe('');
  });

  it('several failing loops with no loop list use the count', () => {
    const v = verdictFor(input({ needs: one({ kind: 'failing_loops', title: '', count: 3, since: ago(3 * HOUR) }) }));
    expect(v.lead).toBe('3 loops on app have been failing for 3h.');
  });

  it('a blocked goal names its title', () => {
    const v = verdictFor(input({ needs: one({ kind: 'blocked_goal', title: 'Ship billing', goal_id: 'g1' }) }));
    expect(v).toEqual({
      lamp: 'stop',
      head: '1 thing needs you',
      lead: 'A goal on app is blocked:',
      rest: 'Ship billing.',
    });
  });

  it('an approval waiting names its kind, its age and its title', () => {
    const v = verdictFor(input({ needs: oneApproval({ kind: 'merge_risky', title: 'Add login' }) }));
    expect(v).toEqual({
      lamp: 'call',
      head: '1 thing needs you',
      lead: 'Merge risky approval on app, asked 2h ago:',
      rest: 'Add login.',
    });
  });

  it('an approval title that already ends in a stop gets no second one', () => {
    const v = verdictFor(input({ needs: oneApproval({ title: 'Add login.' }) }));
    expect(v.rest).toBe('Add login.');
  });
});

describe('verdictFor, the rest of the sentence when more needs wait', () => {
  it('a single approval gives no "Start there"', () => {
    const v = verdictFor(input({ needs: oneApproval({ title: 'Add login' }) }));
    expect(v.rest).not.toContain('Start there');
  });

  it('two needs add "Start there" and "1 more thing follows"', () => {
    const needs = buildNeeds([problem({ title: 'Disk full' })], [approval({})]);
    const v = verdictFor(input({ needs }));
    expect(v.head).toBe('2 things need you');
    expect(v.rest).toBe('Disk full. Start there. 1 more thing follows.');
  });

  it('a title ending in a full stop gets no extra one before "Start there"', () => {
    const needs = buildNeeds([problem({ title: 'Disk full.' })], [approval({})]);
    expect(verdictFor(input({ needs })).rest).toBe('Disk full. Start there. 1 more thing follows.');
  });
});
