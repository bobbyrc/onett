// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { AndonBoard } from './AndonBoard';

const loaded = { data: [], isSuccess: true, isPending: false, isError: false, error: null };

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
    render(
      <MemoryRouter>
        <AndonBoard />
      </MemoryRouter>,
    );
    expect(screen.getByRole('link', { name: 'Projects page' }).getAttribute('href')).toBe('/projects');
    expect(document.body.textContent).not.toContain('onett project add');
  });
});
