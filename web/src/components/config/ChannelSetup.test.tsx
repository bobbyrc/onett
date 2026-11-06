// @vitest-environment jsdom
// The guided setup, rendered for real with react-dom and jsdom and a mocked API. It pins the
// secret handling (a token is sent once and never shown), the PATCH it writes, inline server
// problems and the test-send result.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ConfigDocument } from '../../types';

const api = vi.hoisted(() => ({
  putSecret: vi.fn(),
  patchConfigDocument: vi.fn(),
  listSecrets: vi.fn(),
  sendTest: vi.fn(),
}));

vi.mock('../../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../api')>()), ...api }));

import { ConfigApiError } from '../../api';
import { ChannelSetup } from './ChannelSetup';
import { EntryTest } from './EntryTest';

const TOKEN = '123456:SECRET-TOKEN-VALUE';

let root: Root | undefined;
let host: HTMLElement;

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  api.putSecret.mockResolvedValue({ name: 'TELEGRAM_BOT_TOKEN', set: true, shadowed: false });
  api.patchConfigDocument.mockResolvedValue({ version: 'v2', restart_required: [] });
  api.listSecrets.mockResolvedValue({ secrets: [] });
  api.sendTest.mockResolvedValue({ ok: true });
});

afterEach(() => {
  act(() => root?.unmount());
  root = undefined;
  host?.remove();
  vi.clearAllMocks();
});

const doc = (document: unknown): ConfigDocument => ({ path: '/x/onett.yaml', version: 'v1', document, schema: { key: '', type: 'section' }, default_routing: {} });

async function render(node: React.ReactNode) {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  await act(async () => {
    root?.render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  });
}

function input(label: string): HTMLInputElement {
  const field = [...host.querySelectorAll('label')].find((l) => l.querySelector('.field-label')?.textContent === label);
  const el = field?.querySelector('input');
  if (!el) throw new Error(`no input labelled ${label}`);
  return el;
}

function type(el: HTMLInputElement, value: string) {
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

function button(text: string): HTMLButtonElement {
  const found = [...host.querySelectorAll('button')].find((b) => b.textContent?.startsWith(text));
  if (!found) throw new Error(`no button starting with ${text}`);
  return found;
}

async function click(text: string) {
  await act(async () => {
    button(text).click();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe('Telegram flow', () => {
  it('saves the token as a secret, writes the channel like init, and never shows the token again', async () => {
    await render(<ChannelSetup doc={doc({ notify: [{ type: 'log' }] })} />);
    type(input('Telegram bot token'), TOKEN);
    type(input('Your Telegram user ID'), '42');
    await click('Save & connect');

    expect(api.putSecret).toHaveBeenCalledWith('TELEGRAM_BOT_TOKEN', TOKEN);
    expect(api.patchConfigDocument).toHaveBeenCalledWith('v1', [
      {
        op: 'set',
        path: ['channels'],
        value: [{ type: 'telegram', options: { bot_token_env: 'TELEGRAM_BOT_TOKEN', allowed_user_ids: [42] } }],
      },
    ]);
    // The secret is written before the config entry that needs it.
    expect(api.putSecret.mock.invocationCallOrder[0]).toBeLessThan(api.patchConfigDocument.mock.invocationCallOrder[0]);
    // The input is emptied and the value is nowhere on the page.
    expect(input('Telegram bot token').value).toBe('');
    expect(host.innerHTML).not.toContain('SECRET-TOKEN-VALUE');
    expect(host.textContent).toContain('Saved.');
  });

  it('refuses to save without a token or a numeric ID, and asks nothing of the server', async () => {
    await render(<ChannelSetup doc={doc({})} />);
    type(input('Your Telegram user ID'), 'bob');
    await click('Save & connect');
    expect(api.putSecret).not.toHaveBeenCalled();
    expect(api.patchConfigDocument).not.toHaveBeenCalled();
    expect(host.textContent).toContain('Paste the bot token.');
    expect(host.textContent).toContain('use digits only');
  });

  it('edits the existing channel instead of adding another, and keeps a saved token when the field is empty', async () => {
    api.listSecrets.mockResolvedValue({ secrets: [{ name: 'MY_TG', set: true, source: 'file', referenced_by: [] }] });
    const existing = doc({ channels: [{ type: 'telegram', options: { bot_token_env: 'MY_TG', allowed_user_ids: [1] } }] });
    await render(<ChannelSetup doc={existing} />);
    expect(input('Your Telegram user ID').value).toBe('1');
    type(input('Your Telegram user ID'), '1, 2');
    await click('Save & connect');
    expect(api.putSecret).not.toHaveBeenCalled();
    expect(api.patchConfigDocument).toHaveBeenCalledWith('v1', [
      { op: 'set', path: ['channels', '0', 'options', 'bot_token_env'], value: 'MY_TG' },
      { op: 'set', path: ['channels', '0', 'options', 'allowed_user_ids'], value: [1, 2] },
    ]);
  });

  it('shows the server\'s validation problems inline', async () => {
    api.patchConfigDocument.mockRejectedValue(
      new ConfigApiError(422, 'the config is not valid', [{ path: 'channels.0.options.allowed_user_ids', message: 'must list at least one ID' }]),
    );
    await render(<ChannelSetup doc={doc({})} />);
    type(input('Telegram bot token'), TOKEN);
    type(input('Your Telegram user ID'), '42');
    await click('Save & connect');
    expect(host.textContent).toContain('channels.0.options.allowed_user_ids');
    expect(host.textContent).toContain('must list at least one ID');
    expect(host.textContent).not.toContain('Saved.');
    expect(host.innerHTML).not.toContain('SECRET-TOKEN-VALUE');
  });

  it('shows the result of the test message, success and failure', async () => {
    const saved = doc({ channels: [{ type: 'telegram', options: { bot_token_env: 'TELEGRAM_BOT_TOKEN', allowed_user_ids: [42] } }] });
    api.listSecrets.mockResolvedValue({ secrets: [{ name: 'TELEGRAM_BOT_TOKEN', set: true, source: 'file', referenced_by: [] }] });
    await render(<ChannelSetup doc={saved} />);
    await click('Send test message');
    expect(api.sendTest).toHaveBeenCalledWith('channels', 0);
    expect(host.textContent).toContain('Sent.');

    api.sendTest.mockResolvedValue({ ok: false, error: 'telegram sendMessage: HTTP 403: Forbidden' });
    await click('Send test message');
    expect(host.textContent).toContain('Failed: telegram sendMessage: HTTP 403: Forbidden');
  });

  it('disables the test while there are unsaved edits', async () => {
    const saved = doc({ channels: [{ type: 'telegram', options: { bot_token_env: 'TELEGRAM_BOT_TOKEN', allowed_user_ids: [42] } }] });
    await render(<ChannelSetup doc={saved} />);
    expect(button('Send test message').disabled).toBe(false);
    type(input('Your Telegram user ID'), '43');
    expect(button('Send test message').disabled).toBe(true);
  });
});

describe('Discord flow', () => {
  it('keeps IDs as strings and uses DISCORD_BOT_TOKEN', async () => {
    await render(<ChannelSetup doc={doc({})} />);
    await click('Discord');
    type(input('Discord bot token'), TOKEN);
    type(input('Your Discord user ID'), '123456789012345678');
    await click('Save & connect');
    expect(api.putSecret).toHaveBeenCalledWith('DISCORD_BOT_TOKEN', TOKEN);
    expect(api.patchConfigDocument.mock.calls[0][1][0].value[0]).toEqual({
      type: 'discord',
      options: { bot_token_env: 'DISCORD_BOT_TOKEN', allowed_user_ids: ['123456789012345678'] },
    });
  });
});

describe('ntfy flow', () => {
  it('suggests a random topic, writes the notifier after log, and tests it', async () => {
    await render(<ChannelSetup doc={doc({ notify: [{ type: 'log' }] })} />);
    await click('ntfy');
    const topic = input('Topic').value;
    expect(topic).toMatch(/^onett-[0-9a-f]{16}$/);
    await click('Save');
    expect(api.putSecret).not.toHaveBeenCalled();
    expect(api.patchConfigDocument).toHaveBeenCalledWith('v1', [
      { op: 'set', path: ['notify', '1'], value: { type: 'ntfy', options: { topic } } },
    ]);
  });

  it('stores an optional token as NTFY_TOKEN and names it with token_env', async () => {
    await render(<ChannelSetup doc={doc({ notify: [{ type: 'log' }] })} />);
    await click('ntfy');
    type(input('Server (optional)'), 'https://ntfy.example');
    type(input('ntfy access token'), TOKEN);
    await click('Save');
    expect(api.putSecret).toHaveBeenCalledWith('NTFY_TOKEN', TOKEN);
    const value = api.patchConfigDocument.mock.calls[0][1][0].value;
    expect(value.options).toMatchObject({ server: 'https://ntfy.example', token_env: 'NTFY_TOKEN' });
    expect(input('ntfy access token').value).toBe('');
    expect(host.innerHTML).not.toContain('SECRET-TOKEN-VALUE');
  });

  it('sends a test notification through the saved notifier', async () => {
    const saved = doc({ notify: [{ type: 'log' }, { type: 'ntfy', options: { topic: 'onett-abc' } }] });
    await render(<ChannelSetup doc={saved} />);
    await click('ntfy');
    expect(input('Topic').value).toBe('onett-abc');
    await click('Send test notification');
    expect(api.sendTest).toHaveBeenCalledWith('notify', 1);
  });
});

describe('EntryTest in the visual editor', () => {
  const ctx = (draft: unknown, saved: unknown) => ({ draft, saved, problems: new Map(), setValue: vi.fn(), clearValue: vi.fn() });
  const saved = { notify: [{ type: 'log' }, { type: 'ntfy', options: { topic: 'a' } }] };

  it('sends a test for a saved entry', async () => {
    await render(<EntryTest path={['notify', '1']} ctx={ctx(saved, saved)} />);
    await click('Send test');
    expect(api.sendTest).toHaveBeenCalledWith('notify', 1);
  });

  it('is disabled with "Save first" when the entry has unsaved edits', async () => {
    const draft = { notify: [{ type: 'log' }, { type: 'ntfy', options: { topic: 'b' } }] };
    await render(<EntryTest path={['notify', '1']} ctx={ctx(draft, saved)} />);
    expect(button('Send test').disabled).toBe(true);
    expect(host.textContent).toContain('Save first');
  });

  it('renders nothing for other paths', async () => {
    await render(<EntryTest path={['projects', 'onett']} ctx={ctx(saved, saved)} />);
    expect(host.querySelector('button')).toBeNull();
  });
});
