// @vitest-environment jsdom
import { fireEvent, render, screen, act } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Entry } from '../lib/transcript';
import { Transcript, needsFold } from './Transcript';

const writeText = vi.fn<(t: string) => Promise<void>>();
beforeEach(() => {
  writeText.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
});
afterEach(() => vi.useRealTimers());

const TIME = '2026-11-05T15:04:00Z';
const full = new Date(TIME).toLocaleString();
const view = (entries: Entry[]) => render(<Transcript entries={entries} view="conversation" agentLabel="Orchestrator" />);
const manyLines = Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join('\n');

describe('message rows', () => {
  it('puts the owner message in a right-aligned row and the agent text in a plain one', () => {
    const { container } = view([
      { id: 'u', kind: 'owner', text: 'hi', time: TIME },
      { id: 'a', kind: 'text', text: 'hello', time: TIME },
    ] as Entry[]);
    expect(container.querySelector('.tr-row-you .tr-you')).not.toBeNull();
    expect(container.querySelector('.tr-row-ai .tr-ai')).not.toBeNull();
    expect(container.querySelector('.tr-row-ai .tr-you')).toBeNull();
  });

  it('shows the absolute time in the title and in the accessible text of each timestamp', () => {
    const { container } = view([
      { id: 'u', kind: 'owner', text: 'hi', time: TIME },
      { id: 'a', kind: 'text', text: 'hello', time: TIME },
    ] as Entry[]);
    const times = container.querySelectorAll('time.tr-time');
    expect(times).toHaveLength(2);
    for (const t of times) {
      expect(t.getAttribute('title')).toBe(full);
      expect(t.textContent).toContain(full);
    }
  });

  it('puts the copy button in the action row with the timestamp', () => {
    const { container } = view([{ id: 'u', kind: 'owner', text: 'hi', time: TIME }] as Entry[]);
    const actions = container.querySelector('.tr-actions')!;
    expect(actions.querySelector('time')).not.toBeNull();
    expect(actions.querySelector('button[aria-label="Copy message"]')).not.toBeNull();
  });
});

describe('long owner messages', () => {
  it('folds only above 12 lines or 1200 characters', () => {
    expect(needsFold('x\n'.repeat(11) + 'x')).toBe(false);
    expect(needsFold('x\n'.repeat(12) + 'x')).toBe(true);
    expect(needsFold('x'.repeat(1200))).toBe(false);
    expect(needsFold('x'.repeat(1201))).toBe(true);
  });

  it('has no fold button on a short message', () => {
    view([{ id: 'u', kind: 'owner', text: 'short', time: TIME }] as Entry[]);
    expect(screen.queryByRole('button', { name: /show (more|less)/i })).toBeNull();
  });

  it('toggles aria-expanded and the folded class', () => {
    const { container } = view([{ id: 'u', kind: 'owner', text: manyLines, time: TIME }] as Entry[]);
    const button = screen.getByRole('button', { name: 'Show more' });
    expect(button.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('.tr-body.folded')).not.toBeNull();
    fireEvent.click(button);
    const less = screen.getByRole('button', { name: 'Show less' });
    expect(less.getAttribute('aria-expanded')).toBe('true');
    expect(container.querySelector('.tr-body.folded')).toBeNull();
  });

  it('copies the full text while folded', async () => {
    view([{ id: 'u', kind: 'owner', text: manyLines, time: TIME }] as Entry[]);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy message' }));
    });
    expect(writeText).toHaveBeenCalledWith(manyLines);
  });

  it('folds a task prompt the same way', () => {
    view([{ id: 'p', kind: 'prompt', label: 'Task prompt', text: manyLines, time: TIME }] as Entry[]);
    expect(screen.getByRole('button', { name: 'Show more' })).toBeTruthy();
  });
});
