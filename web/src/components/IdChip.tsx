import { useState } from 'react';
import { copyText } from '../lib/clipboard';
import { shortId } from '../lib/format';

/** The last 8 characters of an ID (the head is a timestamp shared across tasks); clicking copies the full ID. */
export function IdChip({ id }: { id: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    // A failed copy (no clipboard access) leaves the label alone; the title still shows the full ID.
    if (!(await copyText(id))) return;
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };
  return (
    <button type="button" className="id-chip" title={copied ? 'Copied' : `${id} (click to copy)`} onClick={() => void copy()}>
      {copied ? 'copied' : shortId(id)}
    </button>
  );
}
