// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// The activity line and the Send-now row sit in the transcript's centred column, not the pane's left edge.
const rule = (source: string, selector: string): string => {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp(`(?:^|\\n)${esc}\\s*\\{([^}]*)\\}`).exec(source)?.[1] ?? '';
};
const activity = readFileSync(new URL('../components/ActivityLine.css', import.meta.url), 'utf8');
const chat = readFileSync(new URL('../pages/Chat.css', import.meta.url), 'utf8');
const transcript = readFileSync(new URL('../components/Transcript.css', import.meta.url), 'utf8');

describe('column alignment', () => {
  const column = ['width: 100%', 'max-width: var(--chat-max-width)', 'margin: 0 auto'];
  it('uses the same column rule as the transcript', () => {
    for (const decl of column) expect(rule(transcript, '.tr-col')).toContain(decl);
  });
  it('puts the activity line in that column', () => {
    for (const decl of column) expect(rule(activity, '.chat-page .activity-line')).toContain(decl);
  });
  it('puts the Send-now row in that column', () => {
    for (const decl of column) expect(rule(chat, '.chat-page .send-now')).toContain(decl);
  });
});
