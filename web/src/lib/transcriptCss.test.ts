import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// Guards the rules that keep a long tool argument or an unbroken line inside the column.
const css = readFileSync(new URL('../components/Transcript.css', import.meta.url), 'utf8');
const toolCss = readFileSync(new URL('../components/ToolRun.css', import.meta.url), 'utf8');
const rule = (selector: string, source = css): string => {
  const m = new RegExp(`(?:^|\\n)${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`).exec(source);
  return m?.[1] ?? '';
};

describe('transcript layout rules', () => {
  it('lets the column and owner sheets shrink', () => {
    expect(rule('.tr-col')).toContain('min-width: 0');
    expect(rule('.tr-you')).toContain('max-width: min(72ch, 85%)');
  });
  it('ellipses a long tool argument instead of widening the row', () => {
    const decls = ['min-width: 0', 'overflow: hidden'];
    for (const decl of [...decls, 'white-space: nowrap']) expect(rule('.tl-text', toolCss)).toContain(decl);
    for (const decl of [...decls, 'text-overflow: ellipsis']) expect(rule('.tl-arg', toolCss)).toContain(decl);
    for (const decl of [...decls, 'white-space: nowrap', 'text-overflow: ellipsis']) expect(rule('.tl-summary', toolCss)).toContain(decl);
  });
  it('wraps expanded results and terminal lines', () => {
    expect(rule('.tl-body pre', toolCss)).toContain('overflow-wrap: anywhere');
    expect(rule('.tr-tx pre')).toContain('overflow-wrap: anywhere');
  });
});
