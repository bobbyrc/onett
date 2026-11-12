// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import type { Approval, Problem, Task } from '../types';
import type { Need } from '../lib/needs';

const queries = vi.hoisted(() => ({ task: undefined as Task | undefined }));
vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useTask: () => ({ data: queries.task ? { task: queries.task } : undefined }),
}));
vi.mock('./RetryTasksButton', () => ({
  RetryTasksButton: ({ label }: { label: string }) => <button>{label}</button>,
}));

import { NeedRow } from './NeedRow';

const NOW = Date.parse('2026-11-05T12:00:00Z');
const at = (minutesAgo: number) => new Date(NOW - minutesAgo * 60_000).toISOString();

function problemNeed(problem: Problem): Need {
  return { kind: 'problem', key: problem.key, project: problem.project, lamp: 'stop', since: problem.since, problem };
}

function renderRow(need: Need, first = false) {
  return render(
    <MemoryRouter>
      <NeedRow need={need} first={first} now={NOW} />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  queries.task = undefined;
});

describe('NeedRow: blocked tasks', () => {
  const sevenBlocked: Problem = {
    kind: 'blocked_tasks',
    project: 'onett',
    key: 'k1',
    title: 'stuck on the build',
    detail: 'exit status 2: go vet failed',
    count: 7,
    since: at(30),
    last_at: at(5),
    told_at: at(10),
    tasks: [{ id: 't1', title: 'One', attempts: 3, max_attempts: 3 }],
  };

  it('titles a group of blocked tasks, links to the blocked tasks and lists the retries', () => {
    renderRow(problemNeed({ ...sevenBlocked, tasks: Array.from({ length: 7 }, (_, i) => ({ id: `t${i}`, title: `T${i}`, attempts: 1, max_attempts: 3 })) }), true);
    expect(screen.getByText('Start here')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Stuck on the build' }).getAttribute('href')).toBe('/tasks?project=onett&status=blocked');
    expect(document.body.textContent).toContain('7 tasks blocked');
    expect(document.body.textContent).toContain('last blocked');
    expect(screen.getByRole('button', { name: 'Check again' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Retry all 7' })).toBeTruthy();
    const line = document.querySelector('.ov-need-line');
    expect(line?.textContent).toBe('exit status 2: go vet failed');
    expect(line?.classList.contains('mono')).toBe(true);
  });

  it('omits "last blocked" when the last block is less than a minute after the first', () => {
    renderRow(problemNeed({ ...sevenBlocked, last_at: at(29.5) }));
    expect(document.body.textContent).not.toContain('last blocked');
  });

  it('says how many of the blocked tasks the retry covers when only some are listed', () => {
    const tasks = Array.from({ length: 50 }, (_, i) => ({ id: `t${i}`, title: `T${i}`, attempts: 1, max_attempts: 3 }));
    renderRow(problemNeed({ ...sevenBlocked, count: 60, tasks }));
    expect(screen.getByRole('button', { name: 'Retry 50 of 60' })).toBeTruthy();
  });

  it('shows one blocked task with its attempts, its retry and an Open link', () => {
    renderRow(problemNeed({ ...sevenBlocked, count: 1, detail: '', title: 'fix the thing' }));
    expect(screen.getByRole('link', { name: 'Blocked: One' }).getAttribute('href')).toBe('/tasks/t1');
    expect(document.body.textContent).toContain('attempt 3 of 3');
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open' }).getAttribute('href')).toBe('/tasks/t1');
  });

  it('uses the reason as the line when a single blocked task has no detail', () => {
    renderRow(problemNeed({ ...sevenBlocked, count: 1, detail: '', title: 'fix the thing' }));
    const line = document.querySelector('.ov-need-line');
    expect(line?.textContent).toBe('fix the thing');
    expect(line?.classList.contains('mono')).toBe(false);
  });
});

describe('NeedRow: failing loops', () => {
  const loops = (fails: number) => [
    { name: 'watch', fails, since: at(90) },
    { name: 'sweep', fails, since: at(90) },
  ];
  const failing: Problem = {
    kind: 'failing_loops',
    project: 'onett',
    key: 'loops',
    title: '',
    detail: 'timeout',
    count: 2,
    since: at(90),
    loops: loops(13),
  };

  it('counts the failed runs of two loops that failed alike', () => {
    renderRow(problemNeed({ ...failing, told_at: at(20) }));
    expect(screen.getByRole('link', { name: /2 loops are failing/ }).getAttribute('href')).toBe('/loops');
    expect(document.body.textContent).toContain('13 failed runs each');
    expect(document.body.textContent).toContain('told you');
    expect(screen.getByRole('link', { name: 'Open Loops' }).getAttribute('href')).toBe('/loops');
  });

  it('says not told yet when the owner has not been told', () => {
    renderRow(problemNeed(failing));
    expect(document.body.textContent).toContain('not told yet');
  });
});

describe('NeedRow: blocked goal', () => {
  it('links to the goal and offers to open it', () => {
    const goal: Problem = { kind: 'blocked_goal', project: 'onett', key: 'g', title: 'Ship it', detail: '', count: 1, since: at(60), goal_id: 'g1' };
    renderRow(problemNeed(goal));
    expect(screen.getByRole('link', { name: 'Goal blocked: Ship it' }).getAttribute('href')).toBe('/goals/g1');
    expect(screen.getByRole('link', { name: 'Open goal' }).getAttribute('href')).toBe('/goals/g1');
    expect(document.querySelector('.ov-need-line')).toBeNull();
  });
});

describe('NeedRow: approval', () => {
  const approval: Approval = {
    id: 'a1',
    task_id: 't9',
    project: 'onett',
    kind: 'merge',
    title: 'Merge the thing',
    reason: 'Needs   a\nlook',
    status: 'pending',
    created_at: at(15),
  };
  const need: Need = { kind: 'approval', key: 'approval:a1', project: 'onett', lamp: 'call', since: approval.created_at, approval };

  it('shows the risk, the pull request and the reasons the task was rated', () => {
    queries.task = { id: 't9', title: 'x', risk: 'medium', pr_number: 297, risk_reasons: ['Touches auth', 'Large diff'] } as Task;
    renderRow(need);
    expect(screen.getByRole('link', { name: 'Merge approval: Merge the thing' }).getAttribute('href')).toBe('/tasks/t9');
    expect(document.body.textContent).toContain('risk');
    expect(document.body.textContent).toContain('PR #297');
    expect(document.body.textContent).toContain('asked 15m ago');
    expect(document.querySelector('.ov-need-line')?.textContent).toBe('Touches auth. Large diff.');
    expect(document.querySelector('.ov-need-line')?.classList.contains('mono')).toBe(false);
    expect(screen.getByRole('link', { name: 'Review' }).getAttribute('href')).toBe('/tasks/t9');
  });

  it('falls back to the approval reason, with its spacing collapsed, when the task is not loaded', () => {
    renderRow(need);
    expect(document.body.textContent).toContain('asked 15m ago');
    expect(document.body.textContent).not.toContain('PR #');
    expect(document.querySelector('.ov-need-line')?.textContent).toBe('Needs a look');
  });
});
