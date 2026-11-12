// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import type { Problem } from '../types';
import type { Need } from '../lib/needs';

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useTask: () => ({ data: undefined }),
}));
vi.mock('./RetryTasksButton', () => ({
  RetryTasksButton: ({ label }: { label: string }) => <button>{label}</button>,
}));

import { NeedRow } from './NeedRow';

const NOW = Date.parse('2026-11-05T12:00:00Z');
const at = (minutesAgo: number) => new Date(NOW - minutesAgo * 60_000).toISOString();

afterEach(() => {
  cleanup();
});

describe('NeedRow: failing loop detail', () => {
  const failing: Problem = {
    kind: 'failing_loops',
    project: 'onett',
    key: 'failing_loops:onett',
    title: '',
    detail: 'first line\nlast line',
    count: 2,
    since: at(30),
    last_at: at(5),
    loops: [{ name: 'issue_intake', fails: 2, since: at(30) }],
  };

  it('shows the first and last line of the error on separate lines in monospace', () => {
    const need: Need = { kind: 'problem', key: failing.key, project: failing.project, lamp: 'stop', since: failing.since, problem: failing };
    render(
      <MemoryRouter>
        <NeedRow need={need} first={false} now={NOW} />
      </MemoryRouter>,
    );
    const lines = document.querySelectorAll('.ov-need-line.mono');
    expect(lines).toHaveLength(1);
    expect(lines[0].textContent).toBe('first line\nlast line');
  });
});
