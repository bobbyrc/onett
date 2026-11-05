// Stick-to-bottom scrolling for a chat transcript. The reader is "at bottom" when they are within
// NEAR_BOTTOM px of the end of whatever scrolls (the page on desktop and on a phone, or a scrolling
// ancestor if a layout ever gives the transcript its own). While at bottom the view follows new
// content, including a streamed reply growing inside an existing row. Once they scroll up it never
// moves them. Sending a message, opening a conversation or pressing "Jump to latest" scrolls down.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

/** How close to the end still counts as being at the bottom, in px. */
export const NEAR_BOTTOM = 80;
/** A smooth scroll that never reports an end (some browsers lack `scrollend`) is cut off here. */
const SMOOTH_TIMEOUT_MS = 1000;

/** The nearest ancestor that scrolls on its own, else the page's scrolling element. */
function findScroller(el: HTMLElement): HTMLElement {
  for (let p = el.parentElement; p && p !== document.body && p !== document.documentElement; p = p.parentElement) {
    const o = getComputedStyle(p).overflowY;
    if (o === 'auto' || o === 'scroll' || o === 'overlay') return p;
  }
  return (document.scrollingElement as HTMLElement | null) ?? document.documentElement;
}

function distanceFromEnd(s: HTMLElement): number {
  return s.scrollHeight - s.scrollTop - s.clientHeight;
}

function reducedMotion(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

export interface StickToBottom {
  /** Attach to the element that wraps the transcript; its size is what gets watched. */
  contentRef: (el: HTMLElement | null) => void;
  /** False once the reader has scrolled away from the end. */
  atBottom: boolean;
  /** Entries that arrived while the reader was away from the end. */
  unread: number;
  /** Scroll to the end and resume following. Smooth only when the reader allows motion. */
  jumpToLatest: () => void;
  /** Call when the reader sends a message: scrolls to the end now and keeps following. */
  followNow: () => void;
}

/**
 * @param count number of transcript entries, used only for the unread count
 * @param resetKey changes when another conversation opens: the view goes to the end again
 */
export function useStickToBottom(count: number, resetKey: string): StickToBottom {
  const [content, setContent] = useState<HTMLElement | null>(null);
  const [atBottom, setAtBottom] = useState(true);
  const [unread, setUnread] = useState(0);
  const scrollerRef = useRef<HTMLElement | null>(null);
  const stuck = useRef(true);
  // True while our own smooth scroll is running: its in-between scroll events say nothing about the reader.
  const animating = useRef(false);
  const animTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const seen = useRef(count);

  const setStuck = useCallback((v: boolean) => {
    stuck.current = v;
    setAtBottom(v);
    if (v) setUnread(0);
  }, []);

  const scrollEnd = useCallback((smooth: boolean) => {
    const s = scrollerRef.current;
    if (!s) return;
    const top = Math.max(0, s.scrollHeight - s.clientHeight);
    if (smooth && typeof s.scrollTo === 'function') {
      animating.current = true;
      clearTimeout(animTimer.current);
      animTimer.current = setTimeout(() => (animating.current = false), SMOOTH_TIMEOUT_MS);
      s.scrollTo({ top, behavior: 'smooth' });
    } else {
      s.scrollTop = top;
    }
  }, []);

  // New conversation: start following again, with a clean unread count.
  useLayoutEffect(() => {
    stuck.current = true;
    animating.current = false;
    seen.current = count;
    setAtBottom(true);
    setUnread(0);
    scrollEnd(false);
    // count is read once per key on purpose: later growth is handled below.
  }, [resetKey, scrollEnd]);

  useEffect(() => {
    if (!content) return;
    const s = findScroller(content);
    scrollerRef.current = s;
    const isPage = s === document.scrollingElement || s === document.documentElement;
    const target: HTMLElement | Window = isPage ? window : s;

    const onScroll = () => {
      if (animating.current) {
        if (distanceFromEnd(s) <= 1) animating.current = false;
        else return;
      }
      setStuck(distanceFromEnd(s) <= NEAR_BOTTOM);
    };
    const onScrollEnd = () => {
      animating.current = false;
      onScroll();
    };
    target.addEventListener('scroll', onScroll, { passive: true });
    target.addEventListener('scrollend', onScrollEnd);

    // Content grew (new row, streamed text, an image loading): follow only if still at the end.
    // (jsdom has no ResizeObserver; without one the view just stays where it is.)
    const ro =
      typeof ResizeObserver === 'undefined'
        ? undefined
        : new ResizeObserver(() => {
            if (stuck.current) s.scrollTop = Math.max(0, s.scrollHeight - s.clientHeight);
          });
    ro?.observe(content);
    if (stuck.current) s.scrollTop = Math.max(0, s.scrollHeight - s.clientHeight);

    return () => {
      target.removeEventListener('scroll', onScroll);
      target.removeEventListener('scrollend', onScrollEnd);
      ro?.disconnect();
      clearTimeout(animTimer.current);
    };
  }, [content, setStuck]);

  // Entries that landed while the reader was elsewhere.
  useEffect(() => {
    const added = count - seen.current;
    seen.current = count;
    if (added > 0 && !stuck.current) setUnread((u) => u + added);
  }, [count]);

  const jumpToLatest = useCallback(() => {
    setStuck(true);
    scrollEnd(!reducedMotion());
  }, [scrollEnd, setStuck]);

  const followNow = useCallback(() => {
    animating.current = false;
    setStuck(true);
    scrollEnd(false);
  }, [scrollEnd, setStuck]);

  return { contentRef: setContent, atBottom, unread, jumpToLatest, followNow };
}
