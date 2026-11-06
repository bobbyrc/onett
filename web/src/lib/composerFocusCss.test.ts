// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// The composer is a field the owner types in for minutes, so its focused state is a quiet edge, not
// a ring, and nothing in chat focus borrows call yellow, which in onett means "needs you"
// (docs/design/chat-visual.md, "Composer").
const strip = (src: string): string => src.replace(/\/\*[\s\S]*?\*\//g, '');
const composerCss = strip(readFileSync(new URL('../components/Composer.css', import.meta.url), 'utf8'));
const chatCss = strip(readFileSync(new URL('../chat-tokens.css', import.meta.url), 'utf8'));
const appCss = strip(readFileSync(new URL('../styles.css', import.meta.url), 'utf8'));
const darkMarker = 'prefers-color-scheme: dark';

type Decls = Record<string, string>;
function decls(block: string): Decls {
  const out: Decls = {};
  for (const m of block.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
  return out;
}
// Light is everything before the first dark media query; dark overlays the dark block on light.
function modes(src: string): [Decls, Decls] {
  const at = src.indexOf(darkMarker);
  expect(at).toBeGreaterThan(0);
  const light = decls(src.slice(0, at));
  const end = src.indexOf('\n}\n', at);
  return [light, { ...light, ...decls(src.slice(at, end)) }];
}
const [appLight, appDark] = modes(appCss);
const [chatLight, chatDark] = modes(chatCss);

// Follows a token through var() references and returns every name it passes and the final value.
function chain(token: string, d: Decls): { names: string[]; value: string } {
  const names = [token];
  let value = d[token];
  expect(value, token).toBeDefined();
  for (let ref = value.match(/^var\((--[\w-]+)\)$/); ref; ref = value.match(/^var\((--[\w-]+)\)$/)) {
    names.push(ref[1]);
    value = d[ref[1]];
    expect(value, ref[1]).toBeDefined();
  }
  return { names, value: value.toLowerCase() };
}

function rule(selector: string): string {
  const at = composerCss.indexOf(`${selector} {`);
  expect(at, selector).toBeGreaterThanOrEqual(0);
  return composerCss.slice(at, composerCss.indexOf('}', at));
}

describe('composer focus', () => {
  it('steps the card edge up instead of drawing a ring', () => {
    const focused = rule('.chat-page .composer:focus-within');
    expect(focused).toContain('border-color: var(--chat-composer-border-focus)');
    expect(focused).not.toMatch(/outline|box-shadow/);
  });

  for (const [mode, d] of [
    ['light', { ...appLight, ...chatLight }],
    ['dark', { ...appDark, ...chatDark }],
  ] as const) {
    const yellow = chain('--lamp-call', d).value;
    for (const token of ['--chat-composer-border-focus', '--chat-focus-ring']) {
      it(`${mode}: ${token} does not use call yellow`, () => {
        const { names, value } = chain(token, d);
        expect(names).not.toContain('--lamp-call');
        expect(names).not.toContain('--call');
        expect(names).not.toContain('--focus');
        expect(value).not.toBe(yellow);
      });
    }
  }
});
