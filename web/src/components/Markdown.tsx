import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import type { Config } from 'dompurify';
import { highlightCode, isHighlightClass } from '../lib/codeHighlight';
import { safeHref } from '../lib/format';
import { CodeBlockHeader } from './CodeBlock';
import './CodeBlock.css';

// Agent and GitHub text is attacker-controlled. Everything rendered here goes through an explicit
// allowlist: no forms, inputs, images, styles or scripts, and links limited to http(s) and mailto.

const ALLOWED_TAGS = [
  'p', 'br', 'hr', 'a', 'ul', 'ol', 'li', 'code', 'pre', 'em', 'strong', 'del',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'blockquote', 'table', 'thead', 'tbody', 'tr', 'th', 'td',
];

const PURIFY_CONFIG: Config = {
  ALLOWED_TAGS,
  ALLOWED_ATTR: ['href', 'title', 'class'],
  FORBID_ATTR: ['style'],
  ALLOW_DATA_ATTR: false,
};

// marked tags fenced code with a language hint, e.g. class="language-ts". Only that shape is kept.
const LANGUAGE_CLASS = /^language-[\w#+.-]+$/;

const MAILTO = /^mailto:/i;

/** Only http(s) and mailto links survive. Anything else (javascript:, data:, relative) is dropped. */
function safeMarkdownHref(href: string): string | undefined {
  return safeHref(href) ?? (MAILTO.test(href) ? href : undefined);
}

let hooked = false;

function ensureHooks(): void {
  if (hooked) return;
  hooked = true;

  DOMPurify.addHook('uponSanitizeAttribute', (node, data) => {
    if (data.attrName === 'class') {
      // Only code blocks carry a language class, and only the highlighter's own <span> tokens
      // carry hljs-* classes. Neither is a general styling hook.
      const isCode = node.nodeName === 'CODE';
      const isToken = node.nodeName === 'SPAN';
      data.keepAttr = (isCode && LANGUAGE_CLASS.test(data.attrValue)) || (isToken && isHighlightClass(data.attrValue));
    }
  });

  DOMPurify.addHook('afterSanitizeAttributes', (node) => {
    if (!(node instanceof Element) || node.nodeName !== 'A') return;
    const href = node.getAttribute('href');
    if (href === null || safeMarkdownHref(href) === undefined) {
      node.removeAttribute('href');
    }
    node.setAttribute('target', '_blank');
    node.setAttribute('rel', 'noopener noreferrer');
  });
}

/**
 * Sanitises untrusted HTML for the Markdown view. Images are replaced by links to their source
 * (or their alt text when the source is not a safe link). The DOM is parsed in an inert document,
 * so nothing loads or runs during that step.
 */
export function sanitizeMarkdownHTML(html: string): string {
  ensureHooks();

  const doc = new DOMParser().parseFromString(html, 'text/html');
  for (const img of Array.from(doc.querySelectorAll('img'))) {
    const link = doc.createElement('a');
    const src = img.getAttribute('src') ?? '';
    const href = safeMarkdownHref(src);
    if (href !== undefined) link.setAttribute('href', href);
    link.textContent = img.getAttribute('alt') || src || 'image';
    img.replaceWith(link);
  }

  return DOMPurify.sanitize(doc.body.innerHTML, PURIFY_CONFIG);
}

/** Markdown to sanitised HTML. Agent and user text is untrusted, so the sanitiser always runs. */
export function renderMarkdown(source: string): string {
  const html = marked.parse(source, { async: false }) as string;
  return sanitizeMarkdownHTML(html);
}

// Highlighter output: <span class="hljs-*"> and text only. Runs the same hooks as the Markdown
// config, which keep a class on a <span> only when it is a highlight.js token class.
function sanitizeHighlighted(html: string): string {
  ensureHooks();
  return DOMPurify.sanitize(html, { ALLOWED_TAGS: ['span'], ALLOWED_ATTR: ['class'], ALLOW_DATA_ATTR: false });
}

interface Block {
  host: HTMLElement;
  slot: HTMLElement;
  code: HTMLElement;
  lang: string;
}

/** Wraps every `<pre>` in a `.code-block` with a slot for the header bar. Safe to run twice. */
function findBlocks(root: HTMLElement): Block[] {
  const blocks: Block[] = [];
  for (const pre of Array.from(root.querySelectorAll('pre'))) {
    const code = pre.querySelector('code') ?? pre;
    let host = pre.parentElement!;
    if (!host.classList.contains('code-block')) {
      host = document.createElement('div');
      host.className = 'code-block';
      pre.replaceWith(host);
      host.appendChild(pre);
    }
    let slot = host.firstElementChild as HTMLElement;
    if (!slot.classList.contains('code-block-slot')) {
      slot = document.createElement('div');
      slot.className = 'code-block-slot';
      host.prepend(slot);
    }
    const cls = Array.from(code.classList).find((c) => LANGUAGE_CLASS.test(c));
    blocks.push({ host, slot, code, lang: cls ? cls.slice('language-'.length) : 'text' });
  }
  return blocks;
}

export function Markdown({ text }: { text: string }) {
  const html = useMemo(() => renderMarkdown(text), [text]);
  const root = useRef<HTMLDivElement>(null);
  const [blocks, setBlocks] = useState<Block[]>([]);

  // React replaces the whole subtree when the HTML string changes, so the wrappers are rebuilt
  // each time. Done before paint so the header never flashes in.
  useLayoutEffect(() => {
    setBlocks(root.current ? findBlocks(root.current) : []);
  }, [html]);

  // Colour the code once the highlighter chunk has loaded. Until then (and for unknown
  // languages) the plain text stays, so nothing shifts.
  useLayoutEffect(() => {
    let cancelled = false;
    for (const { code, lang } of blocks) {
      if (lang === 'text') continue;
      const source = code.textContent ?? '';
      void highlightCode(source, lang, sanitizeHighlighted).then((out) => {
        if (cancelled || out === null || !code.isConnected || code.textContent !== source) return;
        code.innerHTML = out;
      });
    }
    return () => {
      cancelled = true;
    };
  }, [blocks]);

  return (
    <>
      <div ref={root} className="markdown" dangerouslySetInnerHTML={{ __html: html }} />
      {blocks.map((b, i) =>
        createPortal(
          <CodeBlockHeader lang={b.lang} host={b.host} getText={() => b.code.textContent ?? ''} />,
          b.slot,
          String(i),
        ),
      )}
    </>
  );
}
