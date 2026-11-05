// @vitest-environment jsdom
// Renders the config editor's variant block and the type-change confirmation for real, with
// react-dom and jsdom (no Testing Library: it is not a dependency here).
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { SchemaNode } from '../../types';
import { NodeEditor, type EditContext } from './ConfigSection';

// The secret picker lists secrets; none exist here.
vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  listSecrets: () => Promise.resolve({ secrets: [] }),
}));

const str = (key: string): SchemaNode => ({ key, type: 'string' });
const channel: SchemaNode = {
  key: 'channel',
  type: 'section',
  fields: [
    { key: 'type', type: 'string', enum: ['telegram', 'discord'] },
    {
      key: 'options',
      type: 'variant',
      discriminator: 'type',
      variants: {
        telegram: { key: '', type: 'section', fields: [str('bot_token_env'), str('state_file')] },
        discord: { key: '', type: 'section', fields: [str('bot_token_env'), str('username')] },
        quiet: { key: '', type: 'section', fields: [] },
      },
    },
  ],
};

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
});

function Harness({ initial }: { initial: Record<string, unknown> }) {
  const [draft, setDraft] = useState<unknown>({ channel: initial });
  latest = draft;
  const ctx: EditContext = {
    draft,
    problems: new Map(),
    setValue: (path, value) =>
      setDraft((d: unknown) => {
        const next = structuredClone(d) as Record<string, unknown>;
        let at = next;
        for (const key of path.slice(0, -1)) at = (at[key] ??= {}) as Record<string, unknown>;
        at[path[path.length - 1]] = value;
        return next;
      }),
    clearValue: (path) =>
      setDraft((d: unknown) => {
        const next = structuredClone(d) as Record<string, unknown>;
        let at = next;
        for (const key of path.slice(0, -1)) at = (at[key] ?? {}) as Record<string, unknown>;
        delete at[path[path.length - 1]];
        return next;
      }),
  };
  return <NodeEditor node={channel} path={['channel']} ctx={ctx} nested={true} />;
}

function render(initial: Record<string, unknown>) {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  act(() =>
    root?.render(
      <QueryClientProvider client={client}>
        <Harness initial={initial} />
      </QueryClientProvider>,
    ),
  );
}

function pick(value: string) {
  const select = host.querySelector('select') as HTMLSelectElement;
  act(() => {
    select.value = value;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
}

function button(text: string): HTMLButtonElement {
  const found = [...host.querySelectorAll('button')].find((b) => b.textContent?.startsWith(text));
  if (!found) throw new Error(`no button starting with ${text}`);
  return found;
}

const labels = () => [...host.querySelectorAll('label')].map((l) => l.textContent);
const channelDraft = () => (latest as { channel: Record<string, unknown> }).channel;

describe('VariantEditor', () => {
  it('shows the selected type\'s fields', () => {
    render({ type: 'telegram', options: {} });
    expect(labels()).toContain('State file');
    expect(labels()).not.toContain('Username');
  });

  it('falls back to the JSON editor when the type is unset or unknown', () => {
    render({});
    expect(labels()).toContain('Options (JSON)');
    act(() => root?.unmount());
    host.remove();
    render({ type: 'bogus' });
    expect(labels()).toContain('Options (JSON)');
  });

  it('says so when the type has no options', () => {
    render({ type: 'quiet' });
    expect(host.textContent).toContain('This type has no options.');
  });
});

describe('type change confirmation', () => {
  const start = { type: 'telegram', options: { bot_token_env: 'TOKEN', state_file: '/tmp/s' } };

  it('switches at once when no option would be dropped', () => {
    render({ type: 'telegram', options: { bot_token_env: 'TOKEN' } });
    pick('discord');
    expect(host.querySelector('[role="alert"]')).toBeNull();
    expect(channelDraft().type).toBe('discord');
  });

  it('asks first, and Change drops only the incompatible keys', () => {
    render(start);
    pick('discord');
    const alert = host.querySelector('[role="alert"]');
    expect(alert?.textContent).toContain('drops the options it does not use');
    expect(alert?.textContent).toContain('state_file');
    expect(channelDraft().type).toBe('telegram');
    act(() => button('Change').click());
    expect(host.querySelector('[role="alert"]')).toBeNull();
    expect(channelDraft()).toEqual({ type: 'discord', options: { bot_token_env: 'TOKEN' } });
  });

  it('Keep leaves the type and options untouched', () => {
    render(start);
    pick('discord');
    act(() => button('Keep telegram').click());
    expect(host.querySelector('[role="alert"]')).toBeNull();
    expect(channelDraft()).toEqual(start);
  });
});
