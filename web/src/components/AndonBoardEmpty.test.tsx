// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { AndonBoard } from './AndonBoard';

const loaded = { data: [], isSuccess: true, isPending: false, isError: false, error: null };
const role = vi.hoisted(() => ({ owner: true }));

vi.mock('../auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../auth')>()),
  useCan: () => role.owner,
}));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useProjects: () => loaded,
  useDeployments: () => loaded,
  useControl: () => ({ data: undefined }),
  useStats: () => ({ data: undefined }),
}));

afterEach(cleanup);

describe('AndonBoard empty state', () => {
  it('links to the Projects page and does not mention the CLI', () => {
    role.owner = true;
    render(
      <MemoryRouter>
        <AndonBoard />
      </MemoryRouter>,
    );
    expect(screen.getByRole('link', { name: 'Projects page' }).getAttribute('href')).toBe('/projects');
    expect(document.body.textContent).not.toContain('onett project add');
  });

  it('tells someone who is not an owner that an owner can add one', () => {
    role.owner = false;
    render(
      <MemoryRouter>
        <AndonBoard />
      </MemoryRouter>,
    );
    expect(screen.getByText('No projects yet. An owner can add one on the', { exact: false })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Projects page' }).getAttribute('href')).toBe('/projects');
    expect(document.body.textContent).not.toContain('Add one on the');
  });
});
