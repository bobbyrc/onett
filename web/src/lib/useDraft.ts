import { useCallback, useEffect, useRef, useState } from 'react';

const PREFIX = 'onett.chat.draft.';
const SAVE_DELAY_MS = 300;

/** The localStorage key for one conversation's draft: its id, or `task:<taskId>`. */
export const draftKey = (conversationKey: string) => PREFIX + conversationKey;

// Storage can be missing or refuse (Safari private mode, a full quota). A draft is a convenience,
// so every access swallows the error and the text just stays in memory.
function read(conversationKey: string): string {
  try {
    return window.localStorage.getItem(draftKey(conversationKey)) ?? '';
  } catch {
    return '';
  }
}

function write(conversationKey: string, text: string) {
  try {
    if (text === '') window.localStorage.removeItem(draftKey(conversationKey));
    else window.localStorage.setItem(draftKey(conversationKey), text);
  } catch {
    // Kept in memory only.
  }
}

/**
 * The composer text for one conversation, remembered in this browser. It is restored when the
 * conversation opens, saved shortly after typing stops, and dropped by `clear` (call it after a
 * send succeeds; a failed send keeps the draft). Attachments are not stored.
 */
export function useDraft(conversationKey: string): [string, (text: string) => void, () => void] {
  const [state, setState] = useState(() => ({ key: conversationKey, text: read(conversationKey) }));
  const timer = useRef<number | undefined>(undefined);
  // The latest unsaved text and its key, so a switch or unmount can save it instead of losing it.
  const pending = useRef<{ key: string; text: string } | null>(null);

  const flush = useCallback(() => {
    window.clearTimeout(timer.current);
    if (pending.current) write(pending.current.key, pending.current.text);
    pending.current = null;
  }, []);

  // Switching conversations in place: save what was typed, then load the other draft.
  if (state.key !== conversationKey) {
    flush();
    setState({ key: conversationKey, text: read(conversationKey) });
  }

  useEffect(() => flush, [flush]);

  const set = useCallback(
    (text: string) => {
      setState((s) => ({ ...s, text }));
      pending.current = { key: conversationKey, text };
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(flush, SAVE_DELAY_MS);
    },
    [conversationKey, flush],
  );

  const clear = useCallback(() => {
    window.clearTimeout(timer.current);
    pending.current = null;
    write(conversationKey, '');
    setState((s) => ({ ...s, text: '' }));
  }, [conversationKey]);

  return [state.text, set, clear];
}
