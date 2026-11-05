// @vitest-environment jsdom
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Entry } from '../lib/transcript';
import { Transcript } from './Transcript';

const writeText = vi.fn<(t: string) => Promise<void>>();

beforeEach(() => {
  vi.useFakeTimers();
  writeText.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
});
afterEach(() => vi.useRealTimers());

const md = '**bold** and a [link](https://x)';
const entries = [
  { id: 'u', kind: 'owner', text: 'hello *there*' },
  { id: 'a', kind: 'text', text: md },
] as Entry[];

async function click(button: HTMLElement) {
  await act(async () => {
    fireEvent.click(button);
  });
}

describe('copy message', () => {
  it('copies the raw Markdown of an assistant message and the text of a user message', async () => {
    render(<Transcript entries={entries} view="conversation" agentLabel="Orchestrator" />);
    const [user, assistant] = screen.getAllByRole('button', { name: 'Copy message' });
    await click(assistant);
    expect(writeText).toHaveBeenLastCalledWith(md);
    await click(user);
    expect(writeText).toHaveBeenLastCalledWith('hello *there*');
  });

  it('says Copied for about 1.5 seconds', async () => {
    render(<Transcript entries={entries} view="conversation" agentLabel="Orchestrator" />);
    await click(screen.getAllByRole('button', { name: 'Copy message' })[1]);
    expect(screen.getByText('Copied')).toBeTruthy();
    expect(screen.getByText('Copied').getAttribute('aria-live')).toBe('polite');
    act(() => vi.advanceTimersByTime(1600));
    expect(screen.queryByText('Copied')).toBeNull();
  });

  it('falls back to execCommand when the clipboard API is missing', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
    const exec = vi.fn(() => true);
    (document as unknown as { execCommand: unknown }).execCommand = exec;
    render(<Transcript entries={entries} view="conversation" agentLabel="Orchestrator" />);
    await click(screen.getAllByRole('button', { name: 'Copy message' })[1]);
    expect(exec).toHaveBeenCalledWith('copy');
    expect(screen.getByText('Copied')).toBeTruthy();
  });
});
