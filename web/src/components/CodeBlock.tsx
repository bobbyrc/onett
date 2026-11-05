import { useEffect, useRef, useState } from 'react';
import { copyText } from '../lib/clipboard';

const COPIED_MS = 1500;

/**
 * The header bar of a fenced code block: language label, Copy and Wrap. Markdown renders the
 * block itself as sanitised HTML, so this is portalled into a wrapper around it. `wrap` is
 * mirrored onto that wrapper as a class so CSS can switch the code between scrolling and wrapping.
 */
export function CodeBlockHeader({
  lang,
  getText,
  host,
}: {
  lang: string;
  getText: () => string;
  host: HTMLElement;
}) {
  const [wrap, setWrap] = useState(false);
  const [status, setStatus] = useState<'' | 'Copied' | 'Copy failed'>('');
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);
  useEffect(() => {
    host.classList.toggle('code-block-wrap', wrap);
  }, [host, wrap]);

  const copy = async () => {
    const ok = await copyText(getText());
    setStatus(ok ? 'Copied' : 'Copy failed');
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setStatus(''), COPIED_MS);
  };

  return (
    <div className="code-block-bar">
      <span className="code-block-lang">{lang}</span>
      <span className="code-block-actions">
        <span className="code-block-status" role="status" aria-live="polite">
          {status}
        </span>
        <button type="button" className="code-block-btn" onClick={copy} aria-label={`Copy ${lang} code`}>
          Copy
        </button>
        <button
          type="button"
          className="code-block-btn"
          aria-pressed={wrap}
          aria-label={`Wrap long lines in ${lang} code`}
          onClick={() => setWrap(!wrap)}
        >
          Wrap
        </button>
      </span>
    </div>
  );
}
