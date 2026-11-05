// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// Recomputes the contrast table in docs/design/chat-visual.md from the hex values in chat-tokens.css,
// so a palette edit that breaks a pair fails here instead of in a screenshot review.
const css = readFileSync(new URL('../chat-tokens.css', import.meta.url), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const darkAt = css.indexOf('prefers-color-scheme: dark');

function palette(block: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of block.matchAll(/(--chat-[\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) out[m[1]] = m[2];
  return out;
}
const light = palette(css.slice(0, darkAt));
const dark = { ...light, ...palette(css.slice(darkAt)) };

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

const TEXT = 4.5;
const UI = 3;
const text = ['--chat-fg', '--chat-fg-strong', '--chat-fg-faint'];
const syntax = Object.keys(light).filter((k) => k.startsWith('--chat-syntax-'));
const pairs: [string, string, number][] = [
  ...text.flatMap((fg) => ['--chat-bg', '--chat-surface', '--chat-code-bg'].map((bg): [string, string, number] => [fg, bg, TEXT])),
  ['--chat-user-fg', '--chat-user-bg', TEXT],
  ...['--chat-accent', '--chat-warning', '--chat-danger'].flatMap((fg) =>
    ['--chat-bg', '--chat-surface'].map((bg): [string, string, number] => [fg, bg, TEXT]),
  ),
  ['--chat-warning', '--chat-warning-bg', TEXT],
  ['--chat-diff-add-fg', '--chat-diff-add-bg', TEXT],
  ['--chat-diff-del-fg', '--chat-diff-del-bg', TEXT],
  ['--chat-fg', '--chat-diff-add-bg', TEXT],
  ['--chat-fg', '--chat-diff-del-bg', TEXT],
  ['--chat-diff-add-fg', '--chat-code-bg', TEXT],
  ['--chat-diff-del-fg', '--chat-code-bg', TEXT],
  ...syntax.map((s): [string, string, number] => [s, '--chat-code-bg', TEXT]),
  ...['--chat-bg', '--chat-surface', '--chat-user-bg'].map((bg): [string, string, number] => ['--chat-focus-ring', bg, UI]),
  ['--chat-field-border', '--chat-bg', UI],
  ['--chat-field-border', '--chat-surface', UI],
];

describe('chat token contrast', () => {
  it('parses both palettes', () => {
    expect(darkAt).toBeGreaterThan(0);
    expect(light['--chat-bg']).toBe('#e3eae4');
    expect(dark['--chat-bg']).toBe('#111614');
  });
  for (const [mode, p] of [['light', light], ['dark', dark]] as const) {
    for (const [fg, bg, min] of pairs) {
      it(`${mode}: ${fg} on ${bg} is at least ${min}:1`, () => {
        expect(p[fg], fg).toBeDefined();
        expect(p[bg], bg).toBeDefined();
        expect(contrast(p[fg], p[bg])).toBeGreaterThanOrEqual(min);
      });
    }
  }
});
