import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { cx } from '../lib/format';

/**
 * A compact popover opened by a button. The panel is a plain container (its content may be menu
 * items or a confirmation); Escape closes it and returns focus to the button, a click outside
 * closes it, and ArrowUp/ArrowDown/Home/End move between the panel's `[role^=menuitem]` buttons.
 */
export function MenuPopover({
  label,
  trigger,
  className,
  onClosed,
  children,
}: {
  /** Accessible name of the trigger button. */
  label: string;
  trigger: ReactNode;
  className?: string;
  /** Called whenever the panel is closed (and once on mount), to drop state tied to an open panel. */
  onClosed?: () => void;
  /** Render function so the panel can close itself. */
  children: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLSpanElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const panelId = useId();

  const close = () => {
    setOpen(false);
    button.current?.focus();
  };

  useEffect(() => {
    if (!open) {
      onClosed?.();
      return;
    }
    const outside = (event: MouseEvent) => {
      if (root.current && !root.current.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', outside);
    // Move focus into the panel: the checked item, else the first.
    const items = root.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]');
    const checked = root.current?.querySelector<HTMLElement>('[role^="menuitem"][aria-checked="true"]');
    (checked ?? items?.[0])?.focus();
    return () => document.removeEventListener('mousedown', outside);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') {
      event.stopPropagation();
      close();
      return;
    }
    const items = Array.from(root.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]') ?? []);
    const at = items.indexOf(document.activeElement as HTMLElement);
    const go = (i: number) => {
      event.preventDefault();
      items[(i + items.length) % items.length]?.focus();
    };
    if (event.key === 'ArrowDown') go(at + 1);
    else if (event.key === 'ArrowUp') go(at < 0 ? -1 : at - 1);
    else if (event.key === 'Home') go(0);
    else if (event.key === 'End') go(-1);
  };

  return (
    <span className={cx('menu-pop', className)} ref={root} onKeyDown={open ? onKeyDown : undefined}>
      <button
        ref={button}
        type="button"
        className="menu-pop-trigger"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        onClick={() => setOpen((v) => !v)}
      >
        {trigger}
      </button>
      {open ? (
        <div className="menu-pop-panel" id={panelId} role="menu" aria-label={label}>
          {children(close)}
        </div>
      ) : null}
    </span>
  );
}
