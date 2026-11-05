// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ChatSkeleton, ChatState, SKELETON_DELAY_MS } from './ChatState';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe('ChatState', () => {
  it('renders title, one line of copy and the action', () => {
    const onClick = vi.fn();
    render(
      <ChatState icon="error" title="Couldn't load this conversation" action={<button onClick={onClick}>Retry</button>}>
        boom
      </ChatState>,
    );
    expect(screen.getByRole('heading', { name: "Couldn't load this conversation" })).toBeTruthy();
    expect(screen.getByText('boom')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onClick).toHaveBeenCalledOnce();
    expect(screen.getByRole('alert')).toBeTruthy();
    expect(document.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
  });

  it('is not an alert for an empty state and omits absent copy and action', () => {
    render(<ChatState icon="chat" title="No conversations yet" />);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(document.querySelector('.chat-state-copy')).toBeNull();
    expect(document.querySelector('.chat-state-action')).toBeNull();
  });
});

describe('ChatSkeleton', () => {
  it('is busy with a hidden status at once but draws rows only after the delay', () => {
    render(<ChatSkeleton />);
    const box = screen.getByRole('status');
    expect(box.getAttribute('aria-busy')).toBe('true');
    expect(box.textContent).toBe('Loading conversation');
    expect(box.querySelector('.sr-only')).not.toBeNull();
    expect(document.querySelector('.chat-skel')).toBeNull();
    act(() => {
      vi.advanceTimersByTime(SKELETON_DELAY_MS - 1);
    });
    expect(document.querySelector('.chat-skel')).toBeNull();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(document.querySelectorAll('.chat-skel-user').length).toBeGreaterThan(0);
    expect(document.querySelectorAll('.chat-skel-line').length).toBeGreaterThan(0);
  });
});
