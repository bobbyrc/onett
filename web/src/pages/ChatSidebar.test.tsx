// @vitest-environment jsdom
// The conversation list shows each title on one line and keeps the full title for hover.
import { cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  listConversations: vi.fn(),
  listAgentConversations: vi.fn(),
  listTasks: vi.fn(),
  getConfig: vi.fn(),
  getNewConversationSettings: vi.fn(),
}));

vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => true }));

import { Chat } from './Chat';

const LONG = 'Please investigate why the nightly deploy of the staging environment keeps failing on main';
const conv = (id: string, title: string) => ({ id, title, busy: false, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' });

beforeEach(() => {
  vi.clearAllMocks();
  api.listConversations.mockResolvedValue([conv('c1', LONG), conv('c2', '')]);
  api.listAgentConversations.mockResolvedValue([]);
  api.listTasks.mockResolvedValue([]);
  api.getConfig.mockResolvedValue({});
  api.getNewConversationSettings.mockResolvedValue({ settings: {}, defaults: {}, harness: 'claude', choices: { models: [], efforts: [], permission_modes: [] } });
});
afterEach(cleanup);

it('keeps the full title for hover on each list entry', async () => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<Chat />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  const long = await screen.findByText(LONG);
  expect(long.getAttribute('title')).toBe(LONG);
  expect(screen.getByText('Untitled').getAttribute('title')).toBe('Untitled');
});
