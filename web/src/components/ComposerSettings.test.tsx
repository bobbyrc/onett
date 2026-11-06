// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ComposerSettings } from './ComposerSettings';
import { ContextRing } from './ContextRing';
import type { ConversationSettingsView } from '../types';

const base: ConversationSettingsView = {
  settings: {},
  defaults: { model: 'sonnet' },
  harness: 'claude',
  choices: {
    models: [
      { value: 'opus', label: 'Opus' },
      { value: 'sonnet', label: 'Sonnet' },
    ],
    efforts: [
      { value: 'low', label: 'Low' },
      { value: 'high', label: 'High' },
    ],
    permission_modes: [
      { value: 'auto', label: 'Auto' },
      { value: 'bypassPermissions', label: 'Bypass permissions', dangerous: true },
    ],
  },
};

let stored: ConversationSettingsView;
let puts: Array<Record<string, unknown>>;

beforeEach(() => {
  stored = structuredClone(base);
  puts = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        const body = JSON.parse(String(init.body));
        puts.push(body);
        const { confirm_dangerous: _ignored, ...settings } = body;
        stored = { ...stored, settings };
      }
      return new Response(JSON.stringify(stored), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderPicker(side: 'permission' | 'model') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ComposerSettings conversationId="c1" side={side} />
    </QueryClientProvider>,
  );
}

describe('model and effort picker', () => {
  it('shows the label and saves a model pick', async () => {
    stored.settings = { model: 'opus', effort: 'high' };
    renderPicker('model');
    const trigger = await screen.findByRole('button', { name: /Model and effort: Opus · High/ });
    expect(trigger.textContent).toContain('Opus · High');
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Sonnet' }));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toMatchObject({ model: 'sonnet', effort: 'high', confirm_dangerous: false });
  });

  it('resets model and effort to the role default', async () => {
    stored.settings = { model: 'opus', effort: 'high', permission_mode: 'auto' };
    renderPicker('model');
    fireEvent.click(await screen.findByRole('button', { name: /Model and effort/ }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset to role default' }));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toMatchObject({ model: '', effort: '', permission_mode: 'auto' });
  });

  it('is keyboard operable: arrows move, Escape closes and returns focus', async () => {
    renderPicker('model');
    const trigger = await screen.findByRole('button', { name: /Model and effort/ });
    fireEvent.click(trigger);
    const items = screen.getAllByRole('menuitemradio');
    // The checked item (the role default model) has focus first.
    expect(document.activeElement).toBe(items.find((i) => i.getAttribute('aria-checked') === 'true'));
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Home' });
    expect(document.activeElement).toBe(items[0]);
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(items[1]);
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(trigger);
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
  });
});

describe('permission picker', () => {
  it('saves a normal mode at once', async () => {
    renderPicker('permission');
    fireEvent.click(await screen.findByRole('button', { name: /Permissions: Default permissions/ }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Auto' }));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toMatchObject({ permission_mode: 'auto', confirm_dangerous: false });
  });

  it('asks before choosing a mode that skips safety checks, and sends the confirmation', async () => {
    renderPicker('permission');
    fireEvent.click(await screen.findByRole('button', { name: /Permissions/ }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Bypass permissions/ }));
    expect(puts).toHaveLength(0);
    expect(screen.getByText(/skips safety checks/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(puts).toHaveLength(0);

    fireEvent.click(screen.getByRole('menuitemradio', { name: /Bypass permissions/ }));
    fireEvent.click(screen.getByRole('button', { name: /Allow Bypass permissions/ }));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toMatchObject({ permission_mode: 'bypassPermissions', confirm_dangerous: true });
  });

  it('does not ask again once the mode is set', async () => {
    stored.settings = { permission_mode: 'bypassPermissions' };
    renderPicker('permission');
    fireEvent.click(await screen.findByRole('button', { name: /Permissions: Bypass permissions/ }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Auto' }));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(screen.queryByText(/skips safety checks/)).toBeNull();
  });
});

describe('context ring', () => {
  it('is hidden when the fraction is unknown', () => {
    const { container } = render(<ContextRing fraction={null} />);
    expect(container.firstChild).toBeNull();
  });

  it('names the percentage and marks a nearly full context', () => {
    render(<ContextRing fraction={0.93} />);
    const ring = screen.getByRole('img', { name: 'Context 93% full' });
    expect(ring.getAttribute('data-level')).toBe('high');
  });
});
