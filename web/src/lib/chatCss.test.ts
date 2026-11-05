// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// Chat surfaces read --chat-* tokens (and --lamp-* fills, which are app identity, and the layout-only
// --composer-inset the page sets from JS) only, so contrast is
// measured once in chat-tokens.css. The Terminal view keeps the andon-board look and is cut out by markers.
const files = [
  '../pages/Chat.css',
  '../components/Transcript.css',
  '../components/CopyButton.css',
  '../components/ToolRun.css',
  '../components/ActivityLine.css',
  '../components/ChatState.css',
  '../components/CodeBlock.css',
  '../components/Composer.css',
];

const strip = (src: string): string =>
  src
    .replace(/\/\* terminal-view:start \*\/[\s\S]*?\/\* terminal-view:end \*\//g, '')
    .replace(/\/\*[\s\S]*?\*\//g, '');

describe('chat CSS token guard', () => {
  it('keeps the terminal markers paired in Transcript.css', () => {
    const css = readFileSync(new URL('../components/Transcript.css', import.meta.url), 'utf8');
    expect(css.match(/terminal-view:start/g)?.length).toBe(1);
    expect(css.match(/terminal-view:end/g)?.length).toBe(1);
    expect(css.indexOf('terminal-view:start')).toBeLessThan(css.indexOf('terminal-view:end'));
  });
  for (const f of files) {
    it(`${f.split('/').pop()} has no hex colours and reads only --chat-*, --lamp-* and --composer-inset variables`, () => {
      const css = strip(readFileSync(new URL(f, import.meta.url), 'utf8'));
      expect(css.match(/#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(/g) ?? []).toEqual([]);
      expect(css.match(/var\(--(?!chat-|lamp-|composer-inset\b)[\w-]+/g) ?? []).toEqual([]);
      expect(css.match(/--font-cond/g) ?? []).toEqual([]);
    });
  }
});
