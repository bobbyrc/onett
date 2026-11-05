// @vitest-environment jsdom
// Stick-to-bottom behaviour, with the browser's layout faked: a scrolling div whose height, scroll
// position and size changes the test controls, and a ResizeObserver it can fire by hand.
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { JumpToLatest } from '../components/JumpToLatest';
import { useStickToBottom } from './useStickToBottom';

let fireResize: () => void;
class FakeResizeObserver {
  constructor(private cb: () => void) {
    fireResize = () => this.cb();
  }
  observe() {}
  disconnect() {}
}

/** The faked layout of the scroller: 500px viewport, content of `height` px. */
const box = { height: 2000, top: 0 };

function Harness({ count, id = 'a' }: { count: number; id?: string }) {
  const stick = useStickToBottom(count, id);
  return (
    <div
      data-testid="scroller"
      style={{ overflowY: 'auto' }}
      ref={(el) => {
        if (!el || (el as unknown as { __faked?: boolean }).__faked) return;
        (el as unknown as { __faked: boolean }).__faked = true;
        Object.defineProperty(el, 'scrollHeight', { get: () => box.height });
        Object.defineProperty(el, 'clientHeight', { get: () => 500 });
        Object.defineProperty(el, 'scrollTop', {
          get: () => box.top,
          set: (v: number) => (box.top = v),
          configurable: true,
        });
        el.scrollTo = ((o: ScrollToOptions) => {
          box.top = o.top ?? 0;
          scrolls.push(o.behavior ?? 'auto');
        }) as typeof el.scrollTo;
      }}
    >
      <div ref={stick.contentRef}>content</div>
      <button onClick={stick.followNow}>send</button>
      {!stick.atBottom ? <JumpToLatest unread={stick.unread} onClick={stick.jumpToLatest} /> : null}
    </div>
  );
}

let scrolls: string[];
const END = () => box.height - 500;
const grow = (by: number) => {
  box.height += by;
  act(() => fireResize());
};
const readerScrollsTo = (top: number) => {
  box.top = top;
  fireEvent.scroll(screen.getByTestId('scroller'));
};

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  box.height = 2000;
  box.top = 0;
  scrolls = [];
});
afterEach(() => vi.unstubAllGlobals());

describe('useStickToBottom', () => {
  it('starts at the end of a conversation that is already long', () => {
    render(<Harness count={3} />);
    expect(box.top).toBe(END());
  });

  it('follows growing content while the reader is at the bottom', () => {
    render(<Harness count={3} />);
    grow(300);
    expect(box.top).toBe(END());
    // within 80px of the end still counts as at bottom
    readerScrollsTo(END() - 70);
    grow(100);
    expect(box.top).toBe(END());
    expect(screen.queryByRole('button', { name: /jump to latest/i })).toBeNull();
  });

  it('does not move once the reader scrolled up, and offers the pill with an unread count', () => {
    const { rerender } = render(<Harness count={3} />);
    readerScrollsTo(END() - 400);
    const before = box.top;
    box.height += 200;
    act(() => fireResize());
    rerender(<Harness count={5} />);
    expect(box.top).toBe(before);
    const pill = screen.getByRole('button', { name: /jump to latest/i });
    expect(pill).toHaveTextContent('2 new');
  });

  it('jumps to the end from the pill, hides it, and follows again', () => {
    render(<Harness count={3} />);
    readerScrollsTo(100);
    fireEvent.click(screen.getByRole('button', { name: /jump to latest/i }));
    expect(box.top).toBe(END());
    expect(screen.queryByRole('button', { name: /jump to latest/i })).toBeNull();
    grow(120);
    expect(box.top).toBe(END());
  });

  it('ignores the scroll events of its own smooth scroll', () => {
    vi.stubGlobal('matchMedia', () => ({ matches: false }));
    render(<Harness count={3} />);
    readerScrollsTo(100);
    fireEvent.click(screen.getByRole('button', { name: /jump to latest/i }));
    expect(scrolls).toEqual(['smooth']);
    // a mid-animation event must not flip the state back to "scrolled up"
    readerScrollsTo(END() - 600);
    expect(screen.queryByRole('button', { name: /jump to latest/i })).toBeNull();
    readerScrollsTo(END());
    grow(50);
    expect(box.top).toBe(END());
  });

  it('scrolls instantly when the reader prefers reduced motion', () => {
    vi.stubGlobal('matchMedia', () => ({ matches: true }));
    render(<Harness count={3} />);
    readerScrollsTo(100);
    fireEvent.click(screen.getByRole('button', { name: /jump to latest/i }));
    expect(scrolls).toEqual([]);
    expect(box.top).toBe(END());
  });

  it('scrolls to the end when the reader sends, even from far up', () => {
    render(<Harness count={3} />);
    readerScrollsTo(0);
    fireEvent.click(screen.getByText('send'));
    expect(box.top).toBe(END());
    grow(150); // their message lands
    expect(box.top).toBe(END());
  });

  it('goes to the end again when another conversation opens', () => {
    const { rerender } = render(<Harness count={3} id="a" />);
    readerScrollsTo(0);
    box.height = 4000;
    rerender(<Harness count={9} id="b" />);
    expect(box.top).toBe(END());
    expect(screen.queryByRole('button', { name: /jump to latest/i })).toBeNull();
  });
});
