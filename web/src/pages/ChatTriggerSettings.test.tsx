// @vitest-environment jsdom
// A trigger's conversation shows no settings pickers: its turns run with the orchestrator's
// defaults whatever the conversation stores, so the composer says so instead.
import { cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Conversation, ConversationSettingsView } from '../types';

const api = vi.hoisted(() => ({
  listConversations: vi.fn(),
  listAgentConversations: vi.fn(),
  getConversation: vi.fn(),
  getConversationSettings: vi.fn(),
  listTasks: vi.fn(),
  getConfig: vi.fn(),
  getRun: vi.fn(),
}));

vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => true }));

import { Chat } from './Chat';

const SETTINGS: ConversationSettingsView = {
  settings: {},
  defaults: { model: 'sonnet' },
  harness: 'claude',
  choices: {
    models: [{ value: 'sonnet', label: 'Sonnet' }],
    efforts: [{ value: 'high', label: 'High' }],
    permission_modes: [{ value: 'auto', label: 'Auto' }],
  },
};

const BASE: Conversation = { id: 'c1', title: 'T', busy: false, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' };
const NOTE = "A trigger's turns always run with the orchestrator's default model and permissions, so they cannot be changed here.";

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  api.listConversations.mockResolvedValue([BASE]);
  api.listAgentConversations.mockResolvedValue([]);
  api.listTasks.mockResolvedValue([]);
  api.getConfig.mockResolvedValue({});
  api.getRun.mockResolvedValue({ id: 'r1' });
  api.getConversationSettings.mockResolvedValue(SETTINGS);
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(cleanup);

function renderConversation(conv: Conversation) {
  api.getConversation.mockResolvedValue({ conversation: conv, messages: [] });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/chat/c1']}>
        <Routes>
          <Route path="/chat/:id" element={<Chat />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('settings on a trigger conversation', () => {
  it('shows the fixed-settings note and no pickers', async () => {
    renderConversation({ ...BASE, kind: 'trigger' });
    expect(await screen.findByText(NOTE)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Model and effort/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Permissions/ })).toBeNull();
  });

  it('shows the pickers and no note for an ordinary conversation', async () => {
    renderConversation(BASE);
    expect(await screen.findByRole('button', { name: /Model and effort/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Permissions/ })).toBeInTheDocument();
    expect(screen.queryByText(NOTE)).toBeNull();
  });
});
