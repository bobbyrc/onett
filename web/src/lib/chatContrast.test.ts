// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// Recomputes the contrast table in docs/design/chat-visual.md from chat-tokens.css and the app tokens
// it references in styles.css, so a palette edit that breaks a pair fails here instead of in a
// screenshot review. var() references and color-mix(in srgb, ...) are resolved with a plain sRGB mix.
const strip = (src: string): string => src.replace(/\/\*[\s\S]*?\*\//g, '');
const chatCss = strip(readFileSync(new URL('../chat-tokens.css', import.meta.url), 'utf8'));
const appCss = strip(readFileSync(new URL('../styles.css', import.meta.url), 'utf8'));
const darkMarker = 'prefers-color-scheme: dark';

type Rgb = [number, number, number];
type Decls = Record<string, string>;

// First top-level `:root` block, and the `:root` block inside the dark media query.
function rootDecls(block: string): Decls {
  const out: Decls = {};
  for (const m of block.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim().replace(/\s+/g, ' ');
  return out;
}
function modes(src: string): [Decls, Decls] {
  const at = src.indexOf(darkMarker);
  expect(at).toBeGreaterThan(0);
  const light = rootDecls(src.slice(0, at));
  return [light, { ...light, ...rootDecls(src.slice(at)) }];
}
const [appLight, appDark] = modes(appCss);
const [chatLight, chatDark] = modes(chatCss);

function parseHex(hex: string): Rgb {
  return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)) as Rgb;
}
// Splits "a, b" at top-level commas.
function splitArgs(src: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let cur = '';
  for (const ch of src) {
    if (ch === '(') depth++;
    if (ch === ')') depth--;
    if (ch === ',' && depth === 0) {
      parts.push(cur.trim());
      cur = '';
    } else cur += ch;
  }
  parts.push(cur.trim());
  return parts;
}
function resolve(value: string, decls: Decls): Rgb {
  const v = value.trim();
  if (/^#[0-9a-fA-F]{6}$/.test(v)) return parseHex(v);
  const ref = v.match(/^var\((--[\w-]+)\)$/);
  if (ref) {
    expect(decls[ref[1]], ref[1]).toBeDefined();
    return resolve(decls[ref[1]], decls);
  }
  const mix = v.match(/^color-mix\((.*)\)$/);
  if (mix) {
    const [space, a, b] = splitArgs(mix[1]);
    expect(space).toBe('in srgb');
    const am = a.match(/^(.*) ([\d.]+)%$/);
    expect(am, a).not.toBeNull();
    const w = Number(am![2]) / 100;
    const ca = resolve(am![1], decls);
    const cb = resolve(b, decls);
    return ca.map((c, i) => Math.round(c * w + cb[i] * (1 - w))) as Rgb;
  }
  throw new Error(`cannot resolve ${value}`);
}
// A token that resolves to an opaque colour; transparent mixes (borders) are not measured.
function palette(chat: Decls, app: Decls): Record<string, Rgb> {
  const decls = { ...app, ...chat };
  const out: Record<string, Rgb> = {};
  for (const [k, v] of Object.entries(chat)) {
    if (!k.startsWith('--chat-') || /transparent/.test(v)) continue;
    if (/^(--chat-(font|text|space|radius|msg|max|leading|shadow))/.test(k)) continue;
    out[k] = resolve(v, decls);
  }
  return out;
}
const light = palette(chatLight, appLight);
const dark = palette(chatDark, appDark);

function luminance(rgb: Rgb): number {
  const [r, g, b] = rgb.map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
export function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}
const hex = (rgb: Rgb): string => '#' + rgb.map((v) => v.toString(16).padStart(2, '0')).join('');

const TEXT = 4.5;
const UI = 3;
const text = ['--chat-fg', '--chat-fg-strong', '--chat-fg-faint'];
const syntax = Object.keys(light).filter((k) => k.startsWith('--chat-syntax-'));
const pairs: [string, string, number][] = [
  ...text.flatMap((fg) =>
    ['--chat-bg', '--chat-surface', '--chat-code-bg', '--chat-inline-code-bg'].map((bg): [string, string, number] => [fg, bg, TEXT]),
  ),
  ...['--chat-fg', '--chat-fg-strong'].map((fg): [string, string, number] => [fg, '--chat-user-bg', TEXT]),
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
  ['--chat-composer-border-focus', '--chat-bg', UI],
  ['--chat-composer-border-focus', '--chat-surface', UI],
];

describe('chat token contrast', () => {
  it('parses both palettes', () => {
    expect(hex(light['--chat-bg'])).toBe('#e3eae4');
    expect(hex(dark['--chat-bg'])).toBe('#111614');
  });
  it('resolves the mixes over onett tokens with a plain sRGB mix', () => {
    // 4% of --text (#18201c) over --bg (#e3eae4) in light; 7% of --text (#e4ebe6) over --bg (#111614) in dark.
    expect(hex(light['--chat-user-bg'])).toBe(hex([0, 1, 2].map((i) => Math.round(parseHex('#18201c')[i] * 0.04 + parseHex('#e3eae4')[i] * 0.96)) as Rgb));
    expect(hex(dark['--chat-user-bg'])).toBe(hex([0, 1, 2].map((i) => Math.round(parseHex('#e4ebe6')[i] * 0.07 + parseHex('#111614')[i] * 0.93)) as Rgb));
    for (const k of ['--chat-user-bg', '--chat-code-bg', '--chat-inline-code-bg']) {
      expect(light[k], k).toBeDefined();
      expect(dark[k], k).toBeDefined();
    }
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
