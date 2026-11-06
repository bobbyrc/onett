// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const css = readFileSync(new URL('../pages/Chat.css', import.meta.url), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const rule = (selector: string) => {
  const m = css.match(new RegExp(`(?:^|\\})\\s*${selector.replace(/[.>\s]/g, (c) => `\\${c}`)}\\s*\\{([^}]*)\\}`));
  return m?.[1] ?? '';
};

describe('Chat.css layout', () => {
  it('fixes the page to the viewport so only the lists scroll, never the window', () => {
    expect(rule('.chat-page')).toMatch(/height:\s*calc\(100dvh/);
    expect(css).not.toMatch(/min-height:\s*calc\(100d?vh - 190px\)/);
    expect(rule('.chat-page .messages-scroll')).toMatch(/overflow-y:\s*auto/);
    expect(rule('.chat-page .chat-layout > .conv-list')).toMatch(/overflow-y:\s*auto/);
  });
  it('truncates the branch name to one line', () => {
    const r = rule('.chat-page .thread-branch');
    expect(r).toMatch(/white-space:\s*nowrap/);
    expect(r).toMatch(/text-overflow:\s*ellipsis/);
  });
});
