// @vitest-environment jsdom
// Chat attachments in the orchestrator thread, rendered for real with a mocked API: picking,
// pasting and dropping files, progress and errors, removing a chip, sending the ids, and how a
// sent message shows an image against another file.
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatMessage, Conversation } from '../types';

const api = vi.hoisted(() => ({
  listConversations: vi.fn(),
  listAgentConversations: vi.fn(),
  getConversation: vi.fn(),
  listTasks: vi.fn(),
  getConfig: vi.fn(),
  sendMessage: vi.fn(),
  uploadAttachment: vi.fn(),
}));

vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => true }));

import { ApiError } from '../api';
import { Chat } from './Chat';

const CONV: Conversation = { id: 'c1', title: 'T', busy: false, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' };

interface Pending {
  progress: (f: number) => void;
  resolve: (a: { id: string; name: string; mime: string; size: number }) => void;
  reject: (e: unknown) => void;
  abort: ReturnType<typeof vi.fn>;
  file: File;
}
let uploads: Pending[];
let messages: ChatMessage[];

beforeEach(() => {
  vi.clearAllMocks();
  uploads = [];
  messages = [];
  api.listConversations.mockResolvedValue([CONV]);
  api.listAgentConversations.mockResolvedValue([]);
  api.listTasks.mockResolvedValue([]);
  api.getConfig.mockResolvedValue({ chat: { attachments: { max_per_message: 3 } } });
  api.getConversation.mockImplementation(async () => ({ conversation: CONV, messages }));
  api.sendMessage.mockResolvedValue({});
  api.uploadAttachment.mockImplementation((_id: string, file: File, onProgress: (f: number) => void) => {
    let resolve!: Pending['resolve'];
    let reject!: Pending['reject'];
    const done = new Promise<Parameters<Pending['resolve']>[0]>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    const abort = vi.fn();
    uploads.push({ progress: onProgress, resolve, reject, abort, file });
    return { done, abort };
  });
  Element.prototype.scrollIntoView = vi.fn();
  URL.createObjectURL = vi.fn(() => 'blob:preview');
  URL.revokeObjectURL = vi.fn();
});

function renderChat() {
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

const file = (name: string, type = 'text/plain', body = 'x') => new File([body], name, { type });
const png = (name = 'shot.png') => file(name, 'image/png');

async function ready() {
  renderChat();
  await screen.findByLabelText('Attach files');
}

const pick = (...files: File[]) => fireEvent.change(screen.getByTestId('attach-input'), { target: { files } });
const sendButton = () => screen.getByRole('button', { name: 'Send' });
const result = (id: string, name: string, size = 1) => ({ id, name, mime: 'text/plain', size });

describe('picking files', () => {
  it('shows a chip per file, with a thumbnail only for images, and uploads at once', async () => {
    await ready();
    expect(screen.getByLabelText('Attach files')).toBeInTheDocument();
    pick(png(), file('notes.txt'));
    const chips = within(screen.getByRole('list', { name: 'Attachments' })).getAllByRole('listitem');
    expect(chips).toHaveLength(2);
    expect(chips[0].querySelector('img')).toHaveAttribute('src', 'blob:preview');
    expect(chips[1].querySelector('img')).toBeNull();
    expect(api.uploadAttachment).toHaveBeenCalledTimes(2);
    expect(api.uploadAttachment.mock.calls[0][0]).toBe('c1');
  });

  it('removes a chip, aborts its upload and revokes the thumbnail', async () => {
    await ready();
    pick(png());
    fireEvent.click(screen.getByRole('button', { name: 'Remove shot.png' }));
    expect(screen.queryByRole('list', { name: 'Attachments' })).toBeNull();
    expect(uploads[0].abort).toHaveBeenCalled();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview');
  });

  it('revokes thumbnails on unmount', async () => {
    const view = renderChat();
    await screen.findByLabelText('Attach files');
    pick(png());
    view.unmount();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview');
  });

  it('adds files pasted into the field', async () => {
    await ready();
    fireEvent.paste(screen.getByLabelText('Message the orchestrator'), { clipboardData: { files: [png('pasted.png')], types: ['Files'] } });
    expect(screen.getByRole('button', { name: 'Remove pasted.png' })).toBeInTheDocument();
  });

  it('adds dropped files and shows the drop state while dragging', async () => {
    await ready();
    const form = document.querySelector('form.composer')!;
    fireEvent.dragEnter(form, { dataTransfer: { types: ['Files'] } });
    expect(form).toHaveClass('is-dragging');
    expect(screen.getByText('Drop files to attach')).toBeInTheDocument();
    fireEvent.drop(form, { dataTransfer: { types: ['Files'], files: [file('dropped.txt')] } });
    expect(form).not.toHaveClass('is-dragging');
    expect(screen.getByRole('button', { name: 'Remove dropped.txt' })).toBeInTheDocument();
  });

  it('marks files past the per-message limit instead of uploading them', async () => {
    await ready();
    pick(file('a'), file('b'), file('c'), file('d'));
    expect(api.uploadAttachment).toHaveBeenCalledTimes(3);
    expect(screen.getByRole('alert')).toHaveTextContent('Only 3 files fit in one message');
  });
});

describe('upload state', () => {
  it('shows progress, and keeps Send disabled until the upload finishes', async () => {
    await ready();
    pick(file('a.txt'));
    expect(sendButton()).toBeDisabled();
    act(() => uploads[0].progress(0.5));
    expect(screen.getByRole('progressbar', { name: 'Uploading a.txt' })).toHaveAttribute('value', '50');
    await act(async () => uploads[0].resolve(result('att1', 'a.txt')));
    expect(sendButton()).toBeEnabled();
  });

  it('shows the server error, e.g. too large, and lets the message go without the file', async () => {
    await ready();
    pick(file('big.bin'));
    await act(async () => uploads[0].reject(new ApiError(413, 'the file is larger than chat.attachments.max_size')));
    expect(screen.getByRole('alert')).toHaveTextContent('Too large: the file is larger than chat.attachments.max_size');
    expect(sendButton()).toBeDisabled(); // nothing to send yet
    fireEvent.change(screen.getByLabelText('Message the orchestrator'), { target: { value: 'hi' } });
    expect(sendButton()).toBeEnabled();
  });
});

describe('sending', () => {
  it('passes the uploaded ids, allows empty text, and clears the chips', async () => {
    await ready();
    pick(file('a.txt'), file('b.txt'));
    await act(async () => {
      uploads[0].resolve(result('att1', 'a.txt'));
      uploads[1].resolve(result('att2', 'b.txt'));
    });
    fireEvent.click(sendButton());
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledWith('c1', '', ['att1', 'att2']));
    await waitFor(() => expect(screen.queryByRole('list', { name: 'Attachments' })).toBeNull());
  });
});

describe('sent messages', () => {
  it('renders an image as a lazy thumbnail link and another file as a chip', async () => {
    messages = [
      {
        id: 'm1',
        conversation_id: 'c1',
        role: 'user',
        text: 'look',
        created_at: '2026-01-01T00:00:00Z',
        attachments: [
          { id: 'a1', conversation_id: 'c1', name: 'shot.png', mime: 'image/png', size: 10, created_at: '' },
          { id: 'a2', conversation_id: 'c1', name: 'report.pdf', mime: 'application/pdf', size: 2048, created_at: '' },
        ],
      },
    ];
    renderChat();
    const img = await screen.findByAltText('shot.png');
    expect(img).toHaveAttribute('src', '/api/v1/conversations/c1/attachments/a1');
    expect(img).toHaveAttribute('loading', 'lazy');
    expect(img.closest('a')).toHaveAttribute('target', '_blank');
    const chip = screen.getByRole('link', { name: /report\.pdf/ });
    expect(chip).toHaveAttribute('href', '/api/v1/conversations/c1/attachments/a2');
    expect(chip).toHaveTextContent('2 KB');
  });

  it('falls back to a chip when the image is gone', async () => {
    messages = [
      {
        id: 'm1',
        conversation_id: 'c1',
        role: 'user',
        text: '',
        created_at: '2026-01-01T00:00:00Z',
        attachments: [{ id: 'a1', conversation_id: 'c1', name: 'gone.png', mime: 'image/png', size: 10, created_at: '' }],
      },
    ];
    renderChat();
    fireEvent.error(await screen.findByAltText('gone.png'));
    expect(screen.queryByAltText('gone.png')).toBeNull();
    expect(screen.getByRole('link', { name: /gone\.png/ })).toHaveAttribute('href', '/api/v1/conversations/c1/attachments/a1');
  });
});
