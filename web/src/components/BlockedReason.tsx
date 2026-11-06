import { useLayoutEffect, useRef, useState } from 'react';
import { Callout } from './Callout';

/**
 * A blocked task's reason in a danger box. Agents write long, multi-line reasons (CLI flags,
 * paths), so the box shows about three lines and a "Show more" toggle opens the rest. The toggle
 * appears only when the text really overflows, which is measured, so a short reason has none.
 */
export function BlockedReason({ title, children }: { title?: string; children: string }) {
  const body = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [overflows, setOverflows] = useState(false);

  useLayoutEffect(() => {
    const el = body.current;
    if (!el || open) return;
    const measure = () => setOverflows(el.scrollHeight > el.clientHeight + 1);
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [children, open]);

  return (
    <Callout title={title}>
      <div ref={body} className={open ? 'blocked-reason-text' : 'blocked-reason-text is-clamped'}>
        {children}
      </div>
      {overflows || open ? (
        <button type="button" className="link-button blocked-reason-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? 'Show less' : 'Show more'}
        </button>
      ) : null}
    </Callout>
  );
}
