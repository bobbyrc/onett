// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import type { Approval, Problem, ProjectView, Run } from '../types';
import { buildNeeds } from '../lib/needs';

const loaded = { data: [], isSuccess: true, isPending: false, isError: false, error: null };
const role = vi.hoisted(() => ({ owner: true }));

vi.mock('../auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../auth')>()),
  useCan: () => role.owner,
}));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useProjects: () => ({ ...loaded, data: projects }),
  useDeployments: () => loaded,
  useControl: () => ({ data: undefined }),
  useStats: () => ({ data: undefined }),
  useTask: () => ({ data: undefined }),
  useCancelRun: () => ({ mutate: () => {}, isPending: false, error: null }),
}));

vi.mock('./RetryTasksButton', () => ({
  RetryTasksButton: ({ label }: { label: string }) => <button>{label}</button>,
}));

import { AndonBoard } from './AndonBoard';

const NOW = Date.parse('2026-11-05T12:00:00Z');
const at = (minutesAgo: number) => new Date(NOW - minutesAgo * 60_000).toISOString();

const proj = (name: string, over: Partial<ProjectView> = {}, counts: Partial<ProjectView['counts']> = {}): ProjectView => ({
  name,
  repo: '',
  path: '',
  default_branch: 'main',
  policy: 'default',
  deploy: null,
  observe: [],
  paused: false,
  paused_by: '',
  counts: { queued: 0, running: 0, review: 0, awaiting_approval: 0, blocked: 0, ...counts },
  ...over,
});

let projects: ProjectView[] = [];

afterEach(cleanup);

describe('AndonBoard lanes', () => {
  it('renders a lane per busy project, the needs in each, and folds the idle project into a quiet line', () => {
    projects = [proj('idle'), proj('gamma', {}, { running: 1 }), proj('alpha', {}, { blocked: 7, awaiting_approval: 1 })];
    const problem: Problem = {
      kind: 'blocked_tasks',
      project: 'alpha',
      key: 'p-alpha',
      title: 'stuck',
      detail: '',
      count: 7,
      since: at(30),
      tasks: [{ id: 't1', title: 'One', attempts: 1, max_attempts: 3 }],
    };
    const approval: Approval = {
      id: 'a1',
      task_id: 't9',
      project: 'alpha',
      kind: 'merge',
      title: 'Merge it',
      reason: 'Ready',
      status: 'pending',
      created_at: at(5),
    };
    const run: Run = { id: 'r1', task_id: '', project: 'gamma', role: 'engineer', status: 'running' } as unknown as Run;
    const needs = buildNeeds([problem], [approval]);

    role.owner = true;
    render(
      <MemoryRouter>
        <AndonBoard needs={needs} runs={[run]} scale={600_000} now={NOW} />
      </MemoryRouter>,
    );

    const lanes = Array.from(document.querySelectorAll<HTMLElement>('.ov-lane'));
    expect(lanes).toHaveLength(2);
    expect(within(lanes[0]).getByRole('link', { name: 'Stuck' })).toBeTruthy();
    expect(within(lanes[0]).getByRole('link', { name: 'Merge approval: Merge it' })).toBeTruthy();
    expect(within(lanes[1]).getByText('Nothing needs you here.')).toBeTruthy();
    expect(within(lanes[1]).getByRole('link', { name: 'Chat with Engineer' })).toBeTruthy();
    expect(screen.getAllByText('Start here')).toHaveLength(1);

    const quiet = document.querySelector('.ov-lane-quiet');
    expect(quiet?.textContent).toContain('Quiet:');
    expect(quiet?.textContent).toContain('idle');
  });
});
