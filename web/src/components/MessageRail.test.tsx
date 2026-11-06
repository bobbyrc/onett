// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MessageRail } from './MessageRail';

function Page({ texts }: { texts: string[] }) {
  return (
    <div className="chat-page">
      {texts.map((t, i) => (
        <div key={i}>
          <div className="tr-row tr-row-you">{t}</div>
          <div className="tr-row">reply</div>
        </div>
      ))}
      <MessageRail />
    </div>
  );
}

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(0), 0));
  vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id));
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('MessageRail', () => {
  it('renders one tick per user message', () => {
    render(<Page texts={['a', 'b', 'c']} />);
    expect(screen.getByRole('navigation', { name: 'Messages' })).toBeTruthy();
    expect(screen.getAllByRole('button')).toHaveLength(3);
    expect(screen.getByLabelText('Jump to message 3')).toBeTruthy();
  });

  it('is hidden with fewer than two user messages', () => {
    render(<Page texts={['only one']} />);
    expect(screen.queryByRole('navigation')).toBeNull();
  });

  it('titles a tick with the first 40 characters', () => {
    render(<Page texts={['x'.repeat(60), 'short']} />);
    expect(screen.getByLabelText('Jump to message 1').getAttribute('title')).toBe('x'.repeat(40));
  });

  it('scrolls the message into view on click', () => {
    render(<Page texts={['a', 'b']} />);
    fireEvent.click(screen.getByLabelText('Jump to message 2'));
    const spy = Element.prototype.scrollIntoView as ReturnType<typeof vi.fn>;
    expect(spy).toHaveBeenCalledTimes(1);
    expect(spy.mock.contexts[0]).toBe(document.querySelectorAll('.tr-row-you')[1]);
  });

  it('adds a tick when a message arrives', async () => {
    const { rerender } = render(<Page texts={['a', 'b']} />);
    expect(screen.getAllByRole('button')).toHaveLength(2);
    rerender(<Page texts={['a', 'b', 'c']} />);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(screen.getAllByRole('button')).toHaveLength(3);
  });
});
