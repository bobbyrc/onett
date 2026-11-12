// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { ApiError } from '../api';
import { GoalDetail } from './GoalDetail';
import { RunDetail } from './RunDetail';
import { TaskDetail } from './TaskDetail';

// What the detail query answers; each test sets it.
const state = vi.hoisted(() => ({ result: {} as unknown }));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useTask: () => state.result,
  useRun: () => state.result,
  useGoal: () => state.result,
  usePausedProjects: () => [],
  useCancelRun: () => ({ mutate: () => {}, isPending: false }),
}));

afterEach(cleanup);

const failed = (error: unknown) => ({ isError: true, error, data: undefined });

const show = (path: string, route: string, element: ReactElement) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path={route} element={element} />
      </Routes>
    </MemoryRouter>,
  );

const pages = [
  ['task', '/tasks/nope', '/tasks/:id', <TaskDetail key="t" />, 'Task not found', 'Back to tasks', '/tasks'],
  ['run', '/runs/nope', '/runs/:id', <RunDetail key="r" />, 'Run not found', 'Back to runs', '/runs'],
  ['goal', '/goals/nope', '/goals/:id', <GoalDetail key="g" />, 'Goal not found', 'Back to goals', '/goals'],
] as const;

describe('detail pages with an unknown ID', () => {
  it.each(pages)('%s: a heading and a way back, not the server text', (what, path, route, element, heading, back, href) => {
    state.result = failed(new ApiError(404, `${what} nope: not found`));
    show(path, route, element);
    expect(screen.getByRole('heading', { level: 1, name: heading })).toBeTruthy();
    expect(screen.getByRole('link', { name: back }).getAttribute('href')).toBe(href);
    expect(screen.queryByText(`${what} nope: not found`)).toBeNull();
  });

  it.each(pages)('%s: any other error still shows the error line', (_what, path, route, element, heading) => {
    state.result = failed(new ApiError(500, 'boom'));
    show(path, route, element);
    expect(screen.getByRole('alert').textContent).toContain('boom');
    expect(screen.queryByRole('heading', { name: heading })).toBeNull();
  });
});
