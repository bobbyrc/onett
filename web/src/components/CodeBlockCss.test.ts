// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// Colours live in chat-tokens.css so contrast is measured in one place (docs/design/chat-visual.md).
describe('CodeBlock.css', () => {
  it('has no hard-coded colours; it reads --chat-* tokens only', () => {
    const css = readFileSync(new URL('./CodeBlock.css', import.meta.url), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
    expect(css.match(/#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(/g) ?? []).toEqual([]);
    expect(css.match(/var\(--(?!chat-)[\w-]+/g) ?? []).toEqual([]);
  });
});
