// @vitest-environment jsdom
// Empty, error and not-found states inside the real Chat page.
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  listConversations: vi.fn(),
  listAgentConversations: vi.fn(),
  getConversation: vi.fn(),
  getTaskConversation: vi.fn(),
  listTasks: vi.fn(),
  getConfig: vi.fn(),
  getRun: vi.fn(),
}));

vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
const can = vi.hoisted(() => ({ operate: true }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => can.operate }));

import { ApiError } from '../api';
import { Chat } from './Chat';

function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/chat" element={<Chat />} />
          <Route path="/chat/:id" element={<Chat />} />
          <Route path="/chat/task/:taskId" element={<Chat />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  can.operate = true;
  api.listConversations.mockResolvedValue([]);
  api.listAgentConversations.mockResolvedValue([]);
  api.listTasks.mockResolvedValue([]);
  api.getConfig.mockResolvedValue({});
  api.getRun.mockResolvedValue({ id: 'r1' });
});
afterEach(cleanup);

describe('Chat states', () => {
  it('shows the new-chat heading and copy through ChatState', async () => {
    renderAt('/chat');
    expect(await screen.findByRole('heading', { name: 'Talk to the orchestrator' })).toBeTruthy();
    expect(screen.getByRole('button', { name: "What's running right now?" })).toBeTruthy();
  });

  it('says there are no conversations to a viewer', async () => {
    can.operate = false;
    renderAt('/chat');
    expect(await screen.findByRole('heading', { name: 'No conversations yet' })).toBeTruthy();
  });

  it('asks a viewer to pick a conversation', async () => {
    can.operate = false;
    api.listConversations.mockResolvedValue([
      { id: 'c1', title: 'T', busy: false, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
    ]);
    renderAt('/chat');
    expect(await screen.findByRole('heading', { name: 'Pick a conversation' })).toBeTruthy();
  });

  it('shows not found with a Back action on a 404', async () => {
    api.getTaskConversation.mockRejectedValue(new ApiError(404, 'nope'));
    renderAt('/chat/task/t1');
    expect(await screen.findByRole('heading', { name: 'This task has no conversation' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Back to chat' }).getAttribute('href')).toBe('/chat');
  });

  it('shows a load error with Retry that refetches', async () => {
    api.getConversation.mockRejectedValueOnce(new Error('server exploded'));
    api.getConversation.mockResolvedValue({
      conversation: { id: 'c1', title: 'Fixed', busy: false, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      messages: [],
    });
    renderAt('/chat/c1');
    expect(await screen.findByRole('heading', { name: "Couldn't load this conversation" })).toBeTruthy();
    expect(screen.getByText('server exploded')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(api.getConversation).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByRole('heading', { name: "Couldn't load this conversation" })).toBeNull());
  });
});
