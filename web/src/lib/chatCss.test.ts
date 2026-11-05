// @vitest-environment node
import { existsSync, readFileSync } from 'node:fs';
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

  describe('chat-tokens.css contract', () => {
    const tokens = readFileSync(new URL('../chat-tokens.css', import.meta.url), 'utf8');
    const decl = (name: string): string[] =>
      [...tokens.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(new RegExp(`${name}:\\s*([^;]+);`, 'g'))].map((m) =>
        m[1].trim().replace(/\s+/g, ' '),
      );

    it('sets chat in Geist and mono stacks, not the app font', () => {
      expect(decl('--chat-font')).toEqual(["'Geist', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"]);
      expect(decl('--chat-font-mono')).toEqual(["'SF Mono', SFMono-Regular, ui-monospace, Menlo, monospace"]);
      expect(decl('--chat-font')[0]).not.toMatch(/Barlow|var\(/);
    });
    it('self-hosts Geist at 400, 500 and 600 with font-display: swap', () => {
      for (const w of [400, 500, 600]) {
        expect(tokens).toContain(`./assets/fonts/Geist-${w}.woff2`);
        expect(existsSync(new URL(`../assets/fonts/Geist-${w}.woff2`, import.meta.url))).toBe(true);
      }
      expect(tokens.match(/font-display: swap/g)?.length).toBe(3);
      expect(existsSync(new URL('../assets/fonts/OFL-Geist.txt', import.meta.url))).toBe(true);
    });
    it('uses Orca radius scale', () => {
      expect(decl('--chat-radius')).toEqual(['0.625rem']);
      expect(decl('--chat-radius-sm')).toEqual(['calc(var(--chat-radius) * 0.6)']);
      expect(decl('--chat-radius-md')).toEqual(['calc(var(--chat-radius) * 0.8)']);
      expect(decl('--chat-radius-lg')).toEqual(['var(--chat-radius)']);
      expect(decl('--chat-radius-xl')).toEqual(['calc(var(--chat-radius) * 1.4)']);
    });
    it('keeps the reading column and drops the composer shadow', () => {
      expect(decl('--chat-max-width')).toEqual(['46rem']);
      expect(decl('--chat-shadow-composer')).toEqual(['none']);
    });
    it('defines the derived surfaces as mixes of onett tokens, with Orca percentages', () => {
      const light: Record<string, string> = {
        '--chat-user-bg': 'color-mix(in srgb, var(--text) 4%, var(--bg))',
        '--chat-user-border': 'color-mix(in srgb, var(--text) 7%, transparent)',
        '--chat-code-bg': 'color-mix(in srgb, var(--text) 2%, var(--bg))',
        '--chat-inline-code-bg': 'color-mix(in srgb, var(--text) 4%, var(--bg))',
        '--chat-inline-code-border': 'color-mix(in srgb, var(--text) 9%, transparent)',
        '--chat-composer-border': 'color-mix(in srgb, var(--text) 11%, transparent)',
      };
      for (const [k, v] of Object.entries(light)) expect(decl(k)[0], k).toBe(v);
      // dark block: Orca's dark percentages, over the same onett tokens
      for (const k of Object.keys(light)) {
        expect(decl(k).length, `${k} has a dark override`).toBe(2);
        expect(decl(k)[1], k).toMatch(/^color-mix\(in srgb, var\(--text\) [\d.]+%, (var\(--bg\)|transparent)\)$/);
      }
    });
    it('references onett app tokens for the palette instead of repeating hex', () => {
      const ref: Record<string, string> = {
        '--chat-bg': 'var(--bg)',
        '--chat-surface': 'var(--panel)',
        '--chat-fg-strong': 'var(--text)',
        '--chat-fg-faint': 'var(--muted)',
        '--chat-user-fg': 'var(--text)',
        '--chat-border': 'var(--border)',
        '--chat-field-border': 'var(--field)',
        '--chat-focus-ring': 'var(--focus)',
        '--chat-accent': 'var(--ok)',
        '--chat-warning': 'var(--warn)',
        '--chat-danger': 'var(--danger)',
      };
      for (const [k, v] of Object.entries(ref)) expect(decl(k), k).toEqual([v]);
    });
  });
});
