// @vitest-environment jsdom
// The orchestrator composer inside the real Chat page: drafts survive a reload and are kept or
// cleared by the send outcome, and Send becomes Stop while a turn runs.
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Conversation } from '../types';

const api = vi.hoisted(() => ({
  listConversations: vi.fn(),
  listAgentConversations: vi.fn(),
  getConversation: vi.fn(),
  listTasks: vi.fn(),
  getConfig: vi.fn(),
  sendMessage: vi.fn(),
  cancelRun: vi.fn(),
  getRun: vi.fn(),
}));

vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => true }));

import { Chat } from './Chat';

const CONV: Conversation = { id: 'c1', title: 'T', busy: false, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' };
let conv: Conversation;

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  conv = CONV;
  api.listConversations.mockResolvedValue([CONV]);
  api.listAgentConversations.mockResolvedValue([]);
  api.listTasks.mockResolvedValue([]);
  api.getConfig.mockResolvedValue({});
  api.getRun.mockResolvedValue({ id: 'r1' });
  api.getConversation.mockImplementation(async () => ({ conversation: conv, messages: [] }));
  api.sendMessage.mockResolvedValue({});
  api.cancelRun.mockResolvedValue({});
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(cleanup);

function renderChat(path = '/chat/c1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/chat/:id" element={<Chat />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const field = () => screen.findByLabelText('Message the orchestrator') as Promise<HTMLTextAreaElement>;
const stored = () => window.localStorage.getItem('onett.chat.draft.c1');

describe('chat composer drafts', () => {
  it('restores a saved draft', async () => {
    window.localStorage.setItem('onett.chat.draft.c1', 'half written');
    renderChat();
    expect((await field()).value).toBe('half written');
  });

  it('saves while typing, clears on a successful send and keeps the draft on a failed one', async () => {
    renderChat();
    const box = await field();
    fireEvent.change(box, { target: { value: 'hello' } });
    await waitFor(() => expect(stored()).toBe('hello'));

    api.sendMessage.mockRejectedValueOnce(new Error('nope'));
    fireEvent.keyDown(box, { key: 'Enter' });
    await screen.findByText('nope');
    expect(box.value).toBe('hello');
    expect(stored()).toBe('hello');

    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(box.value).toBe(''));
    expect(stored()).toBeNull();
  });
});

describe('chat composer Stop', () => {
  it('shows Stop while a turn runs and Stopping… after the click', async () => {
    conv = { ...CONV, busy: true, active_run_id: 'r1' };
    renderChat();
    await field();
    // The status bar has a Stop too; the composer's is the one with the icon.
    const stops = await screen.findAllByRole('button', { name: 'Stop' });
    fireEvent.click(stops[stops.length - 1]);
    await waitFor(() => expect(api.cancelRun).toHaveBeenCalledWith('r1'));
    const stopping = await screen.findByRole('button', { name: 'Stopping…' });
    expect(stopping).toBeDisabled();
    expect(stopping).toHaveAttribute('aria-busy', 'true');
  });

  it('offers Send once there is text, even while a turn runs', async () => {
    conv = { ...CONV, busy: true, active_run_id: 'r1' };
    renderChat();
    fireEvent.change(await field(), { target: { value: 'queue me' } });
    expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled();
  });
});
