// @vitest-environment jsdom
// The new-conversation start screen has the thread's composer controls, and what is chosen there
// reaches the conversation the first message creates, before that message is sent.
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Conversation, ConversationSettingsView } from '../types';

const api = vi.hoisted(() => ({
  listConversations: vi.fn(),
  listAgentConversations: vi.fn(),
  getConversation: vi.fn(),
  listTasks: vi.fn(),
  getConfig: vi.fn(),
  createConversation: vi.fn(),
  getNewConversationSettings: vi.fn(),
  getConversationSettings: vi.fn(),
  putConversationSettings: vi.fn(),
  uploadAttachment: vi.fn(),
  sendMessage: vi.fn(),
  getRun: vi.fn(),
}));

vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
const can = vi.hoisted(() => ({ operate: true }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => can.operate }));

import { Chat } from './Chat';

const VIEW: ConversationSettingsView = {
  settings: {},
  defaults: { model: 'opus', effort: 'high', permission_mode: 'default' },
  harness: 'claude',
  choices: {
    models: [{ value: 'opus', label: 'Opus' }, { value: 'sonnet', label: 'Sonnet' }],
    efforts: [{ value: 'high', label: 'High' }, { value: 'low', label: 'Low' }],
    permission_modes: [{ value: 'default', label: 'Ask' }, { value: 'plan', label: 'Plan' }],
  },
};
const NEW: Conversation = { id: 'c9', title: 'New chat', busy: false, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' };

const calls: string[] = [];

beforeEach(() => {
  vi.clearAllMocks();
  calls.length = 0;
  window.localStorage.clear();
  can.operate = true;
  api.listConversations.mockResolvedValue([]);
  api.listAgentConversations.mockResolvedValue([]);
  api.listTasks.mockResolvedValue([]);
  api.getConfig.mockResolvedValue({});
  api.getRun.mockResolvedValue({ id: 'r1' });
  api.getNewConversationSettings.mockResolvedValue(VIEW);
  api.getConversationSettings.mockResolvedValue(VIEW);
  api.getConversation.mockResolvedValue({ conversation: NEW, messages: [] });
  api.createConversation.mockImplementation(async () => {
    calls.push('create');
    return NEW;
  });
  api.putConversationSettings.mockImplementation(async (_id: string, settings: object) => {
    calls.push('put');
    return { ...VIEW, settings };
  });
  api.uploadAttachment.mockImplementation((_id: string, file: File) => {
    calls.push('upload');
    return { done: Promise.resolve({ id: 'a1', name: file.name, size: file.size, mime: file.type }), abort: vi.fn() };
  });
  api.sendMessage.mockImplementation(async () => {
    calls.push('send');
    return {};
  });
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(cleanup);

function renderChat() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="/chat" element={<Chat />} />
          <Route path="/chat/:id" element={<Chat />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('chat start screen composer', () => {
  it('shows the permission and model controls and the attach button', async () => {
    renderChat();
    expect(await screen.findByRole('button', { name: 'Permissions: Ask' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Model and effort: Opus · High' })).toBeTruthy();
    expect(screen.getByTestId('attach-input')).toBeTruthy();
    expect(api.createConversation).not.toHaveBeenCalled();
  });

  it('applies the chosen settings and files to the conversation before the first message runs', async () => {
    renderChat();
    fireEvent.click(await screen.findByRole('button', { name: 'Model and effort: Opus · High' }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: /Sonnet/ }));
    expect(await screen.findByRole('button', { name: 'Model and effort: Sonnet · High' })).toBeTruthy();

    const file = new File(['x'], 'note.txt', { type: 'text/plain' });
    fireEvent.change(screen.getByTestId('attach-input'), { target: { files: [file] } });
    expect(await screen.findByText('note.txt')).toBeTruthy();
    expect(api.uploadAttachment).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText('Message the orchestrator'), { target: { value: 'hello' } });
    fireEvent.keyDown(screen.getByLabelText('Message the orchestrator'), { key: 'Enter' });

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledTimes(1));
    expect(calls).toEqual(['create', 'put', 'upload', 'send']);
    expect(api.putConversationSettings).toHaveBeenCalledWith('c9', { model: 'sonnet' }, true);
    expect(api.sendMessage).toHaveBeenCalledWith('c9', 'hello', ['a1']);
  });

  it('sends with the defaults when nothing was chosen, and only once', async () => {
    renderChat();
    fireEvent.change(await screen.findByLabelText('Message the orchestrator'), { target: { value: 'hi' } });
    fireEvent.keyDown(screen.getByLabelText('Message the orchestrator'), { key: 'Enter' });
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledWith('c9', 'hi', []));
    expect(api.putConversationSettings).not.toHaveBeenCalled();
    expect(api.sendMessage).toHaveBeenCalledTimes(1);
  });

  it('keeps the draft and reuses the created conversation when storing the settings fails', async () => {
    renderChat();
    fireEvent.click(await screen.findByRole('button', { name: 'Permissions: Ask' }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: /Plan/ }));
    const box = screen.getByLabelText('Message the orchestrator') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'go' } });

    api.putConversationSettings.mockRejectedValueOnce(new Error('settings refused'));
    fireEvent.keyDown(box, { key: 'Enter' });
    await screen.findByText('settings refused');
    expect(box.value).toBe('go');
    expect(api.sendMessage).not.toHaveBeenCalled();

    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledTimes(1));
    expect(api.createConversation).toHaveBeenCalledTimes(1);
  });

  it('leaves viewers without a start composer', async () => {
    can.operate = false;
    renderChat();
    expect(await screen.findByText('No conversations yet')).toBeTruthy();
    expect(screen.queryByTestId('attach-input')).toBeNull();
  });
});
