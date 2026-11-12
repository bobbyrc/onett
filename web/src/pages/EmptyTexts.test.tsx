// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { Goals } from './Goals';
import { Runs } from './Runs';
import { Loops } from './Loops';

const loaded = { data: [], isSuccess: true, isPending: false, isError: false, error: null };

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useGoals: () => loaded,
  useRuns: () => loaded,
  useLoops: () => loaded,
  useProjects: () => loaded,
  useRunLoop: () => ({ mutate: () => {}, isPending: false, isSuccess: false, variables: undefined }),
}));

afterEach(cleanup);

function renderAt(url: string, page: React.ReactNode) {
  return render(<MemoryRouter initialEntries={[url]}>{page}</MemoryRouter>);
}

describe('empty-state text', () => {
  it('Goals with no filter explains how to start a goal', () => {
    renderAt('/goals', <Goals />);
    expect(screen.getByText(/No goals yet\./)).toBeTruthy();
    expect(screen.getByText(/onett goal create --project P --title T/)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Chat' }).getAttribute('href')).toBe('/chat');
    expect(screen.queryByText('No goals match these filters.')).toBeNull();
  });

  it('Goals with a filter says nothing matches', () => {
    renderAt('/goals?status=done', <Goals />);
    expect(screen.getByText('No goals match these filters.')).toBeTruthy();
    expect(screen.queryByText(/No goals yet/)).toBeNull();
  });

  it('Runs with no filter explains what a run is', () => {
    renderAt('/runs', <Runs />);
    expect(
      screen.getByText(
        'No runs yet. A run is one agent session: one appears when a task starts or you message an agent in Chat.',
      ),
    ).toBeTruthy();
    expect(screen.queryByText('No runs match these filters.')).toBeNull();
  });

  it('Runs with a filter says nothing matches', () => {
    renderAt('/runs?status=failed', <Runs />);
    expect(screen.getByText('No runs match these filters.')).toBeTruthy();
  });

  it('Loops links to its config section and does not name onett.yaml', () => {
    renderAt('/loops', <Loops />);
    expect(screen.getByRole('link', { name: 'Config, under Loops' }).getAttribute('href')).toBe('/config?section=loops');
    expect(document.body.textContent).not.toContain('onett.yaml');
  });
});
