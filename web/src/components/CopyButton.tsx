import { useEffect, useRef, useState } from 'react';
import { cx } from '../lib/format';
import './CopyButton.css';

/** Copy text with the async clipboard API, or a hidden textarea where that is unavailable (plain http). */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText && window.isSecureContext !== false) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall through to the legacy path (permission denied, or an insecure context).
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0;pointer-events:none';
  document.body.append(ta);
  const prev = document.activeElement as HTMLElement | null;
  ta.select();
  try {
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    ta.remove();
    prev?.focus?.();
  }
}

const FEEDBACK_MS = 1500;

/** An icon button that copies `text` and says "Copied" for a moment (announced through aria-live). */
export function CopyButton({ text, label = 'Copy message', className }: { text: string; label?: string; className?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function onClick() {
    const ok = await copyText(text);
    setState(ok ? 'copied' : 'failed');
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setState('idle'), FEEDBACK_MS);
  }

  const note = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : '';
  return (
    <span className={cx('copy-btn-wrap', state !== 'idle' && 'show', className)}>
      <button type="button" className="copy-btn" aria-label={label} onClick={onClick}>
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.5">
          {state === 'copied' ? (
            <path d="M3 8.5l3.2 3L13 4.5" />
          ) : (
            <>
              <rect x="5.5" y="5.5" width="8" height="8" rx="1" />
              <path d="M10.5 5.5v-2a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2" />
            </>
          )}
        </svg>
      </button>
      <span className="copy-note" role="status" aria-live="polite">
        {note}
      </span>
    </span>
  );
}
