import './ChatState.css';
import { useEffect, useState, type ReactNode } from 'react';

type IconName = 'chat' | 'error' | 'select' | 'missing';

// Square-style line icons on a 24px grid: no circle, no fill, stroke follows the text colour.
const PATHS: Record<IconName, ReactNode> = {
  chat: <path d="M4 5h16v11H10l-5 4v-4H4z" />,
  error: (
    <>
      <path d="M12 4 3 20h18z" />
      <path d="M12 10v5M12 17.5v.5" />
    </>
  ),
  select: (
    <>
      <path d="M4 5h9v6H4zM4 14h9v5H4z" />
      <path d="M17 8h3M17 16h3" />
    </>
  ),
  missing: (
    <>
      <path d="M4 5h16v11H10l-5 4v-4H4z" />
      <path d="m9 8 5 5M14 8l-5 5" />
    </>
  ),
};

/** Empty, error and not-found states for the chat, centred in the reading column. */
export function ChatState({
  icon,
  title,
  children,
  action,
}: {
  icon: IconName;
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="chat-state" role={icon === 'error' ? 'alert' : undefined}>
      <svg
        className="chat-state-icon"
        viewBox="0 0 24 24"
        width="22"
        height="22"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="miter"
        aria-hidden="true"
      >
        {PATHS[icon]}
      </svg>
      <h2 className="chat-state-title">{title}</h2>
      {children ? <p className="chat-state-copy">{children}</p> : null}
      {action ? <div className="chat-state-action">{action}</div> : null}
    </div>
  );
}

/** True once `ms` has passed since mount: keeps a skeleton from flashing on a fast load. */
export function useAfterDelay(ms: number): boolean {
  const [elapsed, setElapsed] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setElapsed(true), ms);
    return () => window.clearTimeout(timer);
  }, [ms]);
  return elapsed;
}

export const SKELETON_DELAY_MS = 150;

/** Placeholder rows shaped like a thread (a right-aligned user sheet, full-width assistant lines), or like the list. */
export function ChatSkeleton({ variant = 'thread', label = 'Loading conversation' }: { variant?: 'thread' | 'list'; label?: string }) {
  const show = useAfterDelay(SKELETON_DELAY_MS);
  // aria-busy stays on from the first render; only the drawing waits for the delay.
  return (
    <div className={`chat-skeleton chat-skeleton-${variant}`} aria-busy="true" role="status">
      <span className="sr-only">{label}</span>
      {show ? (
        <div className="chat-skeleton-rows" aria-hidden="true">
          {variant === 'thread' ? (
            <>
              <span className="chat-skel chat-skel-user" />
              <span className="chat-skel chat-skel-line" />
              <span className="chat-skel chat-skel-line short" />
              <span className="chat-skel chat-skel-user narrow" />
              <span className="chat-skel chat-skel-line" />
            </>
          ) : (
            <>
              <span className="chat-skel chat-skel-item" />
              <span className="chat-skel chat-skel-item" />
              <span className="chat-skel chat-skel-item" />
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}
