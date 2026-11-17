// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  listGithubRepos: vi.fn(),
  inspectProject: vi.fn(),
  discardInspect: vi.fn(),
  createProject: vi.fn(),
  createTask: vi.fn(),
}));
vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));

import { AddProjectDialog } from './AddProjectDialog';

const SIGNED_IN = { gh: { signed_in: true, user: 'bobbyrc', host: 'apple' }, repos: [] as unknown[] };

function result(over: Record<string, unknown> = {}) {
  return {
    name: 'pimlo',
    repo: 'bobbyrc/pimlo',
    github: true,
    private: true,
    copy: { ok: true, error: '' },
    default_branch: 'main',
    branch_from: 'github',
    pull_requests: { ok: true, gh_user: 'bobbyrc', error: '' },
    guess: {
      setup: [{ name: 'npm ci', run: 'npm ci', from: 'package-lock.json' }],
      checks: [{ name: 'npm test', run: 'npm test', from: 'package.json' }],
      top_files: ['package.json'],
    },
    suggest: '',
    ...over,
  };
}

/** A check that reports every step as done and then resolves with the result. */
function checkReturns(r: ReturnType<typeof result>) {
  api.inspectProject.mockImplementation(async (_repo: string, onStep: (s: unknown) => void) => {
    for (const name of ['copy', 'branch', 'pull_requests', 'guess']) onStep({ name, state: 'done' });
    return r;
  });
}

function wrap(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

function open(existing: string[] = []) {
  return wrap(<AddProjectDialog open onClose={() => {}} existing={existing} />);
}

/** Types a repository into the field and starts the check with the Check button. */
function checkRepo(text: string) {
  fireEvent.change(screen.getByPlaceholderText('Search, or paste owner/name or a GitHub link'), { target: { value: text } });
  fireEvent.click(screen.getByRole('button', { name: 'Check' }));
}

beforeEach(() => {
  api.listGithubRepos.mockResolvedValue(SIGNED_IN);
  api.inspectProject.mockReset();
  api.discardInspect.mockResolvedValue(undefined);
  api.createProject.mockResolvedValue({});
  api.createTask.mockResolvedValue({ id: 't1' });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('AddProjectDialog', () => {
  it('lists the repositories from the GitHub CLI and checks the one clicked', async () => {
    api.listGithubRepos.mockResolvedValue({
      gh: { signed_in: true, user: 'bobbyrc', host: 'apple' },
      repos: [
        {
          full_name: 'bobbyrc/pimlo',
          private: true,
          description: 'Photo library organiser',
          default_branch: 'main',
          pushed_at: new Date().toISOString(),
          project: null,
        },
      ],
    });
    api.inspectProject.mockReturnValue(new Promise(() => {}));
    open();
    const repo = await screen.findByRole('button', { name: /bobbyrc\/pimlo/ });
    fireEvent.click(repo);
    expect(api.inspectProject).toHaveBeenCalledWith('bobbyrc/pimlo', expect.any(Function), expect.any(AbortSignal));
  });

  it('shows Needs you for an empty guess and keeps Add off until No setup or checks is chosen', async () => {
    checkReturns(
      result({
        name: 'home-lab',
        repo: 'bobbyrc/home-lab',
        guess: { setup: [], checks: [], top_files: ['README.md', 'Makefile'] },
      }),
    );
    open();
    await screen.findByRole('button', { name: 'Check' });
    checkRepo('bobbyrc/home-lab');
    expect(await screen.findByText('Needs you')).toBeTruthy();
    const add = screen.getByRole('button', { name: 'Add home-lab' }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    expect(screen.getByText('Fill in a command, or choose no setup or checks.')).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: /^No setup or checks/ }));
    await waitFor(() => expect((screen.getByRole('button', { name: 'Add home-lab' }) as HTMLButtonElement).disabled).toBe(false));
  });

  it('shows the gh auth login callout when the GitHub CLI is not signed in', async () => {
    api.listGithubRepos.mockResolvedValue({ gh: { signed_in: false, user: '', host: 'apple', fix: 'gh auth login' }, repos: [] });
    open();
    expect(await screen.findByText('gh auth login')).toBeTruthy();
    expect(screen.getByText('The GitHub CLI is not signed in on this machine.')).toBeTruthy();
  });

  it('discards the copy with its name when Cancel is clicked after a successful check', async () => {
    checkReturns(result());
    open();
    await screen.findByRole('button', { name: 'Check' });
    checkRepo('bobbyrc/pimlo');
    expect(await screen.findByText('Before every task')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(api.discardInspect).toHaveBeenCalledWith('pimlo'));
  });

  it('does not create a task until Start this task is clicked', async () => {
    checkReturns(result());
    open();
    await screen.findByRole('button', { name: 'Check' });
    checkRepo('bobbyrc/pimlo');
    fireEvent.click(await screen.findByRole('button', { name: 'Add pimlo' }));
    expect(await screen.findByText('Ready for work')).toBeTruthy();
    expect(api.createProject).toHaveBeenCalledTimes(1);
    expect(api.createTask).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Start this task' }));
    await waitFor(() => expect(api.createTask).toHaveBeenCalledTimes(1));
    expect(api.createTask).toHaveBeenCalledWith(
      expect.objectContaining({ project: 'pimlo', kind: 'question', title: 'Explain how pimlo is built and tested' }),
    );
  });
});
