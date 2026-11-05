type Core = typeof import('./hljsCore');
let loading: Promise<Core> | null = null;

/** Loads the highlighter on first use. A failed load is retried on the next call. */
function load(): Promise<Core> {
  loading ??= import('./hljsCore').catch((err) => {
    loading = null;
    throw err;
  });
  return loading;
}

// highlight.js marks tokens as <span class="hljs-keyword"> and sub-scopes as "title function_".
const TOKEN_CLASS = /^(?:hljs-[\w-]+|[\w-]+_)$/;

/** Only highlight.js token classes survive, and only on <span>. Used by the Markdown hook. */
export function isHighlightClass(value: string): boolean {
  const tokens = value.split(/\s+/).filter(Boolean);
  return tokens.length > 0 && tokens.every((t) => TOKEN_CLASS.test(t));
}

/**
 * Highlights plain code text and returns sanitised HTML containing only <span class="hljs-*">
 * and text. The input is text (the textContent of an already sanitised block); the output goes
 * through DOMPurify again so a highlighter bug cannot become markup. Resolves to null for an
 * unknown language or when the highlighter cannot load: callers keep the plain code.
 */
export async function highlightCode(
  code: string,
  lang: string,
  sanitize: (html: string) => string,
): Promise<string | null> {
  try {
    const core = await load();
    const html = core.highlightToHTML(code, lang);
    return html === null ? null : sanitize(html);
  } catch {
    return null;
  }
}
