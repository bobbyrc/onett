// A thin rail of ticks at the right edge of the chat, one per user message, for jumping between them.
// It reads the transcript from the DOM (`.tr-row-you` rows) so the transcript's markup stays untouched.
import './MessageRail.css';
import { useEffect, useRef, useState } from 'react';
import { cx } from '../lib/format';

const ROW = '.tr-row-you';

interface Tick {
  el: HTMLElement;
  title: string;
}

function readTicks(root: ParentNode): Tick[] {
  return Array.from(root.querySelectorAll<HTMLElement>(ROW)).map((el) => ({
    el,
    title: (el.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 40),
  }));
}

function reducedMotion(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/** The last message whose top has passed the reading line (40% down the window), else the first. */
function currentIndex(ticks: Tick[]): number {
  const line = window.innerHeight * 0.4;
  let at = 0;
  ticks.forEach((t, i) => {
    if (t.el.getBoundingClientRect().top <= line) at = i;
  });
  return at;
}

/** @param rootSelector the element that holds the transcript; looked up again as the page changes */
export function MessageRail({ rootSelector = '.chat-page' }: { rootSelector?: string }) {
  const [ticks, setTicks] = useState<Tick[]>([]);
  const [current, setCurrent] = useState(0);
  const ticksRef = useRef<Tick[]>([]);

  // Follow the user messages in the transcript as they come and go.
  useEffect(() => {
    const root = document.querySelector(rootSelector);
    if (!root) return;
    let frame = 0;
    const refresh = () => {
      frame = 0;
      const next = readTicks(root);
      const prev = ticksRef.current;
      if (next.length === prev.length && next.every((t, i) => t.el === prev[i].el && t.title === prev[i].title)) return;
      ticksRef.current = next;
      setTicks(next);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(refresh);
    };
    refresh();
    const mo = new MutationObserver(schedule);
    mo.observe(root, { childList: true, subtree: true, characterData: true });
    return () => {
      mo.disconnect();
      if (frame) cancelAnimationFrame(frame);
    };
  }, [rootSelector]);

  // Track which message is being read: a thin band of the window; leaving or entering it re-checks.
  useEffect(() => {
    if (ticks.length < 2) return;
    const update = () => setCurrent(currentIndex(ticks));
    update();
    if (typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver(update, { rootMargin: '-40% 0px -59% 0px' });
    ticks.forEach((t) => io.observe(t.el));
    return () => io.disconnect();
  }, [ticks]);

  if (ticks.length < 2) return null;

  const jump = (t: Tick) => t.el.scrollIntoView({ behavior: reducedMotion() ? 'auto' : 'smooth', block: 'start' });

  return (
    <nav className="msg-rail" aria-label="Messages">
      {ticks.map((t, i) => (
        <button
          key={i}
          type="button"
          className={cx('msg-tick', i === current && 'current')}
          aria-label={`Jump to message ${i + 1}`}
          aria-current={i === current ? 'true' : undefined}
          title={t.title}
          onClick={() => jump(t)}
        >
          <span />
        </button>
      ))}
    </nav>
  );
}
