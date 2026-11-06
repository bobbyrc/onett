// Opening a previous chat on desktop. The sidebar lists every task agent as well as the
// orchestrator chats, so it can be far taller than the transcript. It must not set the page's
// height: once it did, the thread stretched to match, the transcript sat at the top of a page that
// had scrolled to the bottom, and the owner saw only blank space.
import { expect, test } from '@playwright/test';
import type { AgentConversation, ChatMessage, Conversation, LogLine } from '../src/types';
import { RUN_ID } from './fixtures';
import { mockBackend } from './mock';

const T = '2026-11-05T11:50:00Z';
const conv = (id: string, title: string): Conversation => ({ id, title, project: 'onett', busy: false, created_at: T, updated_at: T });
const NEWER = conv('conv-newer', 'Newer chat');
const OLDER = conv('conv-older', 'Older chat');

// Sixty task agents: far more rows than a window shows.
const AGENTS: AgentConversation[] = Array.from({ length: 60 }, (_, i) => ({
  task_id: `task-${i}`,
  title: `Task agent number ${i}`,
  project: 'onett',
  role: 'implementer',
  task_status: 'done',
  live: false,
  closed: true,
  updated_at: T,
}));

const messages = (c: string): ChatMessage[] => [
  { id: `${c}-1`, conversation_id: c, role: 'user', text: `Question in ${c}`, created_at: T, run_id: RUN_ID },
  { id: `${c}-2`, conversation_id: c, role: 'assistant', text: `Answer in ${c}`, created_at: T, run_id: RUN_ID },
];
const LOGS: LogLine[] = [{ run_id: RUN_ID, seq: 1, time: T, kind: 'text', text: 'Answer from the older chat' }];

test.use({ viewport: { width: 1280, height: 800 } });

test('opening a previous chat shows its messages and a page no taller than its content', async ({ page }) => {
  await mockBackend(page);
  await page.route('**/api/v1/agent-conversations', (r) => r.fulfill({ json: AGENTS }));
  await page.route('**/api/v1/conversations', (r) => r.fulfill({ json: [NEWER, OLDER] }));
  await page.route('**/api/v1/conversations/*', (r) => {
    const id = new URL(r.request().url()).pathname.split('/').pop();
    r.fulfill({ json: { conversation: id === OLDER.id ? OLDER : NEWER, messages: messages(id ?? '') } });
  });
  await page.route('**/api/v1/runs/*/logs*', (r) => r.fulfill({ json: LOGS }));

  await page.goto(`/chat/${NEWER.id}`);
  await page.getByRole('link', { name: /Older chat/ }).click();

  const question = page.getByText(`Question in ${OLDER.id}`);
  await expect(question).toBeInViewport();
  await expect(page.getByText('Answer from the older chat')).toBeInViewport();

  // A short transcript plus a composer is about one window tall; the list's 60 rows are not.
  const { scrollHeight, innerHeight } = await page.evaluate(() => ({
    scrollHeight: document.documentElement.scrollHeight,
    innerHeight: window.innerHeight,
  }));
  expect(scrollHeight).toBeLessThan(innerHeight * 1.5);
});
