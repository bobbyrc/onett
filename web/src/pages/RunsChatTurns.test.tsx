// @vitest-environment jsdom
// A run with no task is a chat turn: Runs and Overview name it for the agent, with no task chip and
// no "Loading task…". A run with a task still shows its task title.
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Run } from '../types';

const api = vi.hoisted(() => ({ listRuns: vi.fn(), getTask: vi.fn(), listProjects: vi.fn(), getRun: vi.fn() }));
vi.mock('../components/LogViewer', () => ({ LogViewer: () => null }));
vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => true }));

import { Runs } from './Runs';
import { RunItem } from './Overview';
import { RunDetail } from './RunDetail';
import { Route, Routes } from 'react-router-dom';

const base = {
  id: 'run-1', task_id: '', project: 'onett', role: 'chief-of-staff', harness: 'claude', status: 'running',
  started_at: '2026-11-05T10:00:00Z', last_output_at: '2026-11-05T10:00:00Z', exit_code: 0, cost_usd: 0,
  input_tokens: 0, output_tokens: 0,
} as Run;
const taskRun: Run = { ...base, id: 'run-2', task_id: 'task-1', role: 'engineer' };

function wrap(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  api.listProjects.mockResolvedValue([]);
  api.getTask.mockResolvedValue({ task: { id: 'task-1', title: 'Fix the thing' } });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('Runs page', () => {
  it('names a task-less run for the agent and shows no task chip', async () => {
    api.listRuns.mockResolvedValue([base]);
    wrap(<Runs />);
    const link = await screen.findByRole('link', { name: 'Chat with Chief of Staff' });
    expect(link.getAttribute('href')).toBe('/runs/run-1');
    expect(screen.queryByText('Loading task…')).toBeNull();
    expect(document.querySelector('a[href^="/tasks/"]')).toBeNull();
    expect(document.querySelector('.run-task-meta')?.textContent).toBe('onett');
    expect(api.getTask).not.toHaveBeenCalled();
  });

  it('shows the task title for a run with a task', async () => {
    api.listRuns.mockResolvedValue([taskRun]);
    wrap(<Runs />);
    expect(await screen.findByRole('link', { name: 'Fix the thing' })).toBeTruthy();
    expect(document.querySelector('a[href="/tasks/task-1"]')).not.toBeNull();
  });
});

describe('Overview live run', () => {
  it('names a task-less run for the agent without a dangling separator', () => {
    wrap(<RunItem run={base} scale={600000} now={Date.parse('2026-11-05T10:01:00Z')} />);
    expect(screen.getByRole('link', { name: 'Chat with Chief of Staff' })).toBeTruthy();
    expect(screen.queryByText('Loading task…')).toBeNull();
    expect(document.querySelector('.run-item-meta span')?.textContent).toBe('onett');
  });

  it('shows the task title and agent for a run with a task', async () => {
    wrap(<RunItem run={taskRun} scale={600000} now={Date.parse('2026-11-05T10:01:00Z')} />);
    expect(screen.getByText('Loading task…')).toBeTruthy();
    await waitFor(() => expect(screen.getByRole('link', { name: 'Fix the thing' })).toBeTruthy());
    expect(document.querySelector('.run-item-meta span')?.textContent).toMatch(/ · onett$/);
  });
});

describe('chat run links to its conversation', () => {
  const chatRun: Run = { ...base, conversation_id: 'conv-1' };

  it('Runs links the label to the conversation', async () => {
    api.listRuns.mockResolvedValue([chatRun]);
    wrap(<Runs />);
    const link = await screen.findByRole('link', { name: 'Chat with Chief of Staff' });
    expect(link.getAttribute('href')).toBe('/chat/conv-1');
  });

  it('Overview links the label to the conversation', () => {
    wrap(<RunItem run={chatRun} scale={600000} now={Date.parse('2026-11-05T10:01:00Z')} />);
    expect(screen.getByRole('link', { name: 'Chat with Chief of Staff' }).getAttribute('href')).toBe('/chat/conv-1');
  });

  const legacyRun: Run = { ...base };

  it('Runs falls back to the run page without a conversation_id', async () => {
    api.listRuns.mockResolvedValue([legacyRun]);
    wrap(<Runs />);
    const link = await screen.findByRole('link', { name: 'Chat with Chief of Staff' });
    expect(link.getAttribute('href')).toBe('/runs/run-1');
  });

  it('Overview falls back to the run page without a conversation_id', () => {
    wrap(<RunItem run={legacyRun} scale={600000} now={Date.parse('2026-11-05T10:01:00Z')} />);
    expect(screen.getByRole('link', { name: 'Chat with Chief of Staff' }).getAttribute('href')).toBe('/runs/run-1');
  });

  it('RunDetail shows no Conversation link without a conversation_id', async () => {
    api.getRun.mockResolvedValue(legacyRun);
    renderDetail();
    await screen.findByText(/chief-of-staff|Chief of Staff/i);
    expect(screen.queryByRole('link', { name: 'Conversation' })).toBeNull();
  });

  it('RunDetail shows a Conversation link when conversation_id is set', async () => {
    api.getRun.mockResolvedValue(chatRun);
    renderDetail();
    const link = await screen.findByRole('link', { name: 'Conversation' });
    expect(link.getAttribute('href')).toBe('/chat/conv-1');
  });
});

function renderDetail() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/runs/run-1']}>
        <Routes>
          <Route path="/runs/:id" element={<RunDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
