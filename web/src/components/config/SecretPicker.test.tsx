// @vitest-environment jsdom
// The secret picker for secret_env fields, rendered for real with react-dom and jsdom.
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { SecretPicker, suggestSecretName } from './SecretPicker';

const api = vi.hoisted(() => ({
  listSecrets: vi.fn(),
  putSecret: vi.fn(),
}));
vi.mock('../../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../api')>()), ...api }));

const TOKEN_VALUE = 'sup3r-secret-value';

let root: Root | undefined;
let host: HTMLElement;
let latest: unknown;

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});

afterEach(() => {
  act(() => root?.unmount());
  root = undefined;
  host?.remove();
  vi.clearAllMocks();
});

function Harness({ initial }: { initial?: string }) {
  const [value, setValue] = useState<unknown>(initial);
  latest = value;
  return <SecretPicker id="f" fieldKey="bot_token_env" type="telegram" value={value} onChange={setValue} />;
}

async function render(initial?: string) {
  api.listSecrets.mockResolvedValue({
    secrets: [
      { name: 'TELEGRAM_BOT_TOKEN', set: false, source: '', referenced_by: [] },
      { name: 'NTFY_TOKEN', set: true, source: 'file', referenced_by: [] },
    ],
  });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root?.render(
      <QueryClientProvider client={client}>
        <Harness initial={initial} />
      </QueryClientProvider>,
    );
  });
  // Let the secrets query settle.
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function type(input: HTMLInputElement, text: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
  act(() => {
    setter?.call(input, text);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

function button(text: string): HTMLButtonElement {
  const found = [...host.querySelectorAll('button')].find((b) => b.textContent?.startsWith(text));
  if (!found) throw new Error(`no button starting with ${text}`);
  return found;
}

describe('SecretPicker', () => {
  it('lists the known names with their status', async () => {
    await render();
    const options = [...host.querySelectorAll('datalist option')].map((o) => [o.getAttribute('value'), o.textContent]);
    expect(options).toEqual(
      expect.arrayContaining([
        ['TELEGRAM_BOT_TOKEN', 'not set'],
        ['NTFY_TOKEN', 'set'],
      ]),
    );
  });

  it('suggests a name for an empty field and fills it on request', async () => {
    await render();
    expect(host.textContent).toContain('TELEGRAM_BOT_TOKEN');
    act(() => button('Use it').click());
    expect(latest).toBe('TELEGRAM_BOT_TOKEN');
    expect(host.querySelector('[data-testid="secret-status"]')?.textContent).toBe('not set');
  });

  it('sets a new secret with PUT, clears the input and never renders the value', async () => {
    api.putSecret.mockResolvedValue({ name: 'TELEGRAM_BOT_TOKEN', set: true, shadowed: false });
    await render('TELEGRAM_BOT_TOKEN');
    act(() => button('Set value').click());
    const input = host.querySelector('input[type="password"]') as HTMLInputElement;
    type(input, TOKEN_VALUE);
    // Typing is not fetched or echoed anywhere as text.
    expect(host.textContent).not.toContain(TOKEN_VALUE);
    await act(async () => button('Save').click());
    expect(api.putSecret).toHaveBeenCalledWith('TELEGRAM_BOT_TOKEN', TOKEN_VALUE);
    expect(host.querySelector('input[type="password"]')).toBeNull();
    expect(host.textContent).toContain('Secret saved. Save the config to use it.');
    expect(host.textContent).not.toContain(TOKEN_VALUE);
    // The name was not changed by saving the secret.
    expect(latest).toBe('TELEGRAM_BOT_TOKEN');
  });

  it('offers Replace for a name that is set and reports a shadowed save', async () => {
    api.putSecret.mockResolvedValue({ name: 'NTFY_TOKEN', set: true, shadowed: true });
    await render('NTFY_TOKEN');
    act(() => button('Replace').click());
    type(host.querySelector('input[type="password"]') as HTMLInputElement, 'x');
    await act(async () => button('Save').click());
    expect(host.textContent).toContain("shadowed by the daemon's environment");
  });

  it('rejects an invalid name and offers no way to set it', async () => {
    await render();
    type(host.querySelector('input.config-input') as HTMLInputElement, 'bad-name');
    expect(host.textContent).toContain('Use capital letters, digits and underscores');
    expect(host.textContent).not.toContain('Set value');
    expect(api.putSecret).not.toHaveBeenCalled();
  });
});

describe('suggestSecretName', () => {
  it('builds a name from the type and key', () => {
    expect(suggestSecretName('bot_token_env', undefined, 'telegram')).toBe('TELEGRAM_BOT_TOKEN');
    expect(suggestSecretName('webhook_url_env', undefined, 'discord')).toBe('DISCORD_WEBHOOK_URL');
    expect(suggestSecretName('token_env', undefined, 'ntfy')).toBe('NTFY_TOKEN');
    expect(suggestSecretName('token_env', undefined, undefined)).toBe('TOKEN');
  });

  it('prefers a name the help gives', () => {
    expect(suggestSecretName('token_env', 'Defaults to MY_SPECIAL_TOKEN. Optional.', 'ntfy')).toBe('MY_SPECIAL_TOKEN');
  });
});
