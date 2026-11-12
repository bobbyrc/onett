import { useEffect, useId, useRef, useState, type ReactNode } from 'react';

type Props = {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
};

// What Tab can stop on.
// shortcut: does not check that a control is visible; upgrade if a dialog ever holds a hidden one.
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** A dialog over the page. While it is open, keyboard focus stays inside it. */
export function Modal({ open, ...rest }: Props) {
  // The dialog is mounted only while open, so its mount and unmount are the open and the close.
  return open ? <Dialog {...rest} /> : null;
}

function Dialog({ title, onClose, children, wide }: Omit<Props, 'open'>) {
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  // What had focus when the dialog opened. Read during the first render, because a field with
  // autoFocus takes focus as it mounts, before any effect runs.
  const [opener] = useState(() => document.activeElement);

  // Focus moves into the dialog when it opens and back to the opener when it closes.
  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.contains(document.activeElement)) {
      (dialog.querySelector('.modal-body')?.querySelector<HTMLElement>(FOCUSABLE) ?? dialog).focus();
    }
    return () => {
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, [opener]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') return onClose();
      const dialog = dialogRef.current;
      if (event.key !== 'Tab' || !dialog) return;
      // Tab stays inside: it wraps at either end, and comes back in if focus got outside.
      const stops = [...dialog.querySelectorAll<HTMLElement>(FOCUSABLE)];
      const first = stops[0] ?? dialog;
      const last = stops[stops.length - 1] ?? dialog;
      const at = document.activeElement;
      if (!dialog.contains(at) || at === dialog || at === (event.shiftKey ? first : last)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      className="modal-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        className={wide ? 'modal modal-wide' : 'modal'}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
      >
        <header className="modal-header">
          <h2 id={titleId}>{title}</h2>
          <button type="button" className="btn btn-ghost" aria-label="Close" onClick={onClose}>
            ×
          </button>
        </header>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  );
}
