// Chat screenshots for PR descriptions (npm run shots:chat). Four scenes, each in four variants
// (desktop/iPhone x light/dark), written to e2e-results/chat-shots/<scene>-<variant>.png. The data
// is defined here, not in fixtures.ts, so the shots read like a real conversation and stay stable
// when other fixtures change. scripts/pr-shots.sh publishes the PNGs.
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { deflateSync } from 'node:zlib';
import type { ChatMessage, Conversation, LogLine, TaskConversation } from '../src/types';
import { CHAT_TASK_ID, CONV_ID, RUN_ID, tasks, runs } from './fixtures';
import { mockBackend } from './mock';

const OUT = 'e2e-results/chat-shots';

const VARIANTS = [
  { name: 'desktop-light', scheme: 'light', viewport: { width: 1280, height: 800 }, dpr: 1, mobile: false },
  { name: 'desktop-dark', scheme: 'dark', viewport: { width: 1280, height: 800 }, dpr: 1, mobile: false },
  { name: 'iphone-light', scheme: 'light', viewport: { width: 390, height: 844 }, dpr: 2, mobile: true },
  { name: 'iphone-dark', scheme: 'dark', viewport: { width: 390, height: 844 }, dpr: 2, mobile: true },
] as const;

// The clock is frozen a few minutes after the newest message, so "5m ago" never drifts.
const FROZEN = '2026-11-05T12:05:00Z';
const T1 = '2026-11-05T11:50:00Z';
const T2 = '2026-11-05T11:52:00Z';
const T3 = '2026-11-05T11:58:00Z';

const msg = (n: number, conv: string, role: 'user' | 'assistant', text: string, time: string, run?: string): ChatMessage => ({
  id: `shot-msg-${n}`,
  conversation_id: conv,
  role,
  text,
  created_at: time,
  run_id: run,
});

const log = (seq: number, kind: LogLine['kind'], text: string, time: string, tool_id?: string): LogLine => ({
  run_id: RUN_ID,
  seq,
  time,
  kind,
  text,
  tool_id,
});

// A 640x400 gradient PNG, built here so the shot shows a real thumbnail (the mock's default
// download is 1x1). Each scanline starts with filter byte 0.
function gradientPng(w: number, h: number): Buffer {
  const raw = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++) {
    const row = y * (w * 3 + 1);
    for (let x = 0; x < w; x++) {
      raw[row + 1 + x * 3] = Math.round((x / w) * 255);
      raw[row + 2 + x * 3] = Math.round((y / h) * 255);
      raw[row + 3 + x * 3] = 160;
    }
  }
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (b: Buffer) => {
    let c = 0xffffffff;
    for (const x of b) c = crcTable[(c ^ x) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc(body));
    return Buffer.concat([len, body, sum]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // RGB
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}
const SHOT_PNG = gradientPng(640, 400);

// A short thread whose first message carries a screenshot, a PDF and an SVG: the screenshot is a
// thumbnail, the others file tiles.
const ATTACHED: ChatMessage[] = [
  {
    ...msg(40, CONV_ID, 'user', 'Here is the failing dashboard, and the notes I was working from.', T1),
    attachments: [
      { id: 'shot-att-1', conversation_id: CONV_ID, name: 'screenshot.png', mime: 'image/png', size: 84_000, created_at: T1 },
      { id: 'shot-att-3', conversation_id: CONV_ID, name: 'shepherd-notes.pdf', mime: 'application/pdf', size: 2_400_000, created_at: T1 },
      { id: 'shot-att-4', conversation_id: CONV_ID, name: 'diagram.svg', mime: 'image/svg+xml', size: 3_100, created_at: T1 },
    ],
  },
  msg(41, CONV_ID, 'assistant', 'Thanks, I can see the screenshot. The table overflows at narrow widths; I will open a task for it.', T2),
];

const THREAD: ChatMessage[] = [
  msg(1, CONV_ID, 'user', 'Why does the shepherd loop retry a failed merge every tick? Can you show me where it decides?', T1, RUN_ID),
  msg(3, CONV_ID, 'user', 'Thanks. Open a task to cap that backoff at 10 minutes.', T3),
];

// Tool calls between the first user message and the answer, so the tool run sits mid-thread.
const ANSWER =
  'The retry comes from `shepherdPR`: it re-reads the PR state each tick and only backs off for a **denied** merge.\n\n' +
  '```go\nif pr.State == scm.StateOpen && !pr.Mergeable {\n\treturn s.requeue(task, backoff(task.Attempts))\n}\n```\n\n' +
  'A conflicted PR is `Mergeable == false`, so it takes the requeue path with a growing backoff rather than a fixed one.';

// The run's log: the reply is its last text line (the transcript shows it, not the message).
const THREAD_LOGS: LogLine[] = [
  log(1, 'tool_use', 'Grep: {"pattern":"func (e \\*Engine) shepherdPR"}', '2026-11-05T11:51:00Z', 'a1'),
  log(2, 'tool_result', 'internal/engine/loop_shepherd.go:112', '2026-11-05T11:51:01Z', 'a1'),
  log(3, 'tool_use', 'Read: {"file_path":"internal/engine/loop_shepherd.go"}', '2026-11-05T11:51:02Z', 'a2'),
  log(4, 'tool_result', 'package engine ...', '2026-11-05T11:51:03Z', 'a2'),
  log(5, 'tool_use', 'Bash: go test ./internal/engine -run TestShepherd', '2026-11-05T11:51:30Z', 'a3'),
  log(6, 'tool_result', 'ok  github.com/bobbyrc/onett/internal/engine 0.8s', '2026-11-05T11:51:40Z', 'a3'),
  log(7, 'text', ANSWER, T2),
];

// The task thread's run log. Seqs are unique (they make the entry ids), and the follow-up is an
// owner line in the log, as the agent sees it, so the message rail has two user turns to tick.
const TASK_LOGS: LogLine[] = [
  log(1, 'prompt', 'Cap the shepherd backoff at 10 minutes and add a test.', T1),
  ...THREAD_LOGS.slice(0, 6).map((l) => ({ ...l, seq: l.seq + 1 })),
  log(8, 'text', 'Done: `backoff` now returns `min(d, 10*time.Minute)`, with `TestBackoffCapped` covering it.', T2),
  log(9, 'owner', 'Also log when the cap kicks in.', T3),
  log(10, 'tool_use', 'Edit: {"file_path":"internal/engine/loop_shepherd.go"}', '2026-11-05T11:58:30Z', 'c1'),
  log(11, 'tool_result', 'ok', '2026-11-05T11:58:40Z', 'c1'),
  log(12, 'tool_use', 'Bash: go test ./internal/engine', '2026-11-05T11:59:00Z', 'c2'),
  log(13, 'tool_result', 'ok  github.com/bobbyrc/onett/internal/engine 0.9s', '2026-11-05T11:59:10Z', 'c2'),
  log(14, 'text', 'Added a log line when `backoff` hits the cap.', '2026-11-05T12:00:00Z'),
];

const RUNNING_LOGS: LogLine[] = [
  log(1, 'tool_use', 'Read: {"file_path":"internal/engine/loop_shepherd.go"}', '2026-11-05T12:03:00Z', 'b1'),
  log(2, 'tool_result', 'package engine ...', '2026-11-05T12:03:01Z', 'b1'),
  log(3, 'text', 'Reading the backoff helper to see where the cap belongs', '2026-11-05T12:04:30Z'),
  // A call still running, with the agent's progress heartbeat reporting how long it has gone.
  log(4, 'tool_use', 'Bash: go test -race ./internal/engine', '2026-11-05T12:04:40Z', 'b2'),
  log(5, 'system', '{"type":"tool_progress","tool_use_id":"b2","tool_name":"Bash","elapsed_time_seconds":47}', '2026-11-05T12:05:27Z'),
];

const conv = (over: Partial<Conversation>): Conversation => ({
  id: CONV_ID,
  title: 'Shepherd retry backoff',
  project: 'onett',
  busy: false,
  created_at: T1,
  updated_at: T3,
  ...over,
});

const taskConv: TaskConversation = {
  task: tasks[0],
  conversation: conv({ id: 'shot-task-conv', task_id: tasks[0].id, title: 'Cap the shepherd backoff' }),
  tasks: [tasks[0]],
  runs: runs.slice(0, 1),
  messages: [
    msg(11, 'shot-task-conv', 'user', 'Cap the shepherd backoff at 10 minutes and add a test.', T1),
  ],
  can_send: true,
};

const json = (page: Page, glob: string, body: unknown) =>
  page.route(glob, (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
      : route.fallback(),
  );

type Scene = { name: string; path: string; setup: (page: Page) => Promise<void>; ready: string; /** Scroll up before the shot, so the Jump to latest pill shows. */ away?: boolean; /** Open every "Worked for" fold and the first tool run, so the quiet tool lines show. */ open?: boolean; /** Click the button with this name prefix before the shot. */ pick?: string };

const SCENES: Scene[] = [
  {
    name: 'chat-thread',
    path: `/chat/${CONV_ID}`,
    ready: '.composer',
    setup: async (page) => {
      await json(page, '**/api/v1/conversations/*', { conversation: conv({}), messages: THREAD });
      await json(page, '**/api/v1/runs/*/logs*', THREAD_LOGS);
    },
  },
  {
    // Image attachments in a sent message: thumbnails for images, tiles for other files.
    name: 'chat-attachments',
    path: `/chat/${CONV_ID}`,
    ready: '.composer',
    setup: async (page) => {
      await json(page, '**/api/v1/conversations/*', { conversation: conv({}), messages: ATTACHED });
      // A real-sized image instead of the mock's 1x1.
      await page.route('**/api/v1/conversations/*/attachments/*', (route) =>
        route.fulfill({ status: 200, contentType: 'image/png', body: SHOT_PNG }),
      );
    },
  },
  {
    name: 'chat-thread-open',
    path: `/chat/${CONV_ID}`,
    ready: '.composer',
    open: true,
    setup: async (page) => {
      await json(page, '**/api/v1/conversations/*', { conversation: conv({}), messages: THREAD });
      await json(page, '**/api/v1/runs/*/logs*', THREAD_LOGS);
    },
  },
  {
    // A long thread read from the top: the Jump to latest pill floats above the composer.
    name: 'chat-jump',
    path: `/chat/${CONV_ID}`,
    ready: '.composer',
    away: true,
    setup: async (page) => {
      const long = Array.from({ length: 6 }, (_, i) => msg(20 + i, CONV_ID, 'user', `Question ${i + 1}: ${THREAD[0].text}`, T1));
      await json(page, '**/api/v1/conversations/*', { conversation: conv({}), messages: [...long, ...THREAD] });
      await json(page, '**/api/v1/runs/*/logs*', THREAD_LOGS);
    },
  },
  {
    name: 'chat-running',
    path: `/chat/${CONV_ID}`,
    ready: '.composer',
    setup: async (page) => {
      const live = conv({ busy: true, active_run_id: RUN_ID });
      await json(page, '**/api/v1/conversations', [live]);
      await json(page, '**/api/v1/conversations/*', { conversation: live, messages: THREAD.slice(0, 1) });
      await json(page, '**/api/v1/runs/*/logs*', RUNNING_LOGS);
    },
  },
  {
    name: 'task-thread',
    path: `/chat/task/${CHAT_TASK_ID}`,
    ready: '.composer',
    setup: async (page) => {
      await json(page, '**/api/v1/tasks/*/conversation*', taskConv);
      await json(page, '**/api/v1/runs/*/logs*', TASK_LOGS);
    },
  },
  {
    name: 'task-thread-open',
    path: `/chat/task/${CHAT_TASK_ID}`,
    ready: '.composer',
    open: true,
    setup: async (page) => {
      await json(page, '**/api/v1/tasks/*/conversation*', taskConv);
      await json(page, '**/api/v1/runs/*/logs*', TASK_LOGS);
    },
  },
  {
    // The model and effort picker open above the composer footer.
    name: 'composer-picker',
    path: `/chat/${CONV_ID}`,
    ready: '.composer',
    pick: 'Model and effort',
    setup: async () => {},
  },
  {
    name: 'chat-empty',
    path: '/chat',
    ready: '.composer',
    setup: async (page) => {
      await json(page, '**/api/v1/conversations', []);
    },
  },
  {
    name: 'chat-error',
    path: `/chat/${CONV_ID}`,
    ready: '.chat-state',
    setup: async (page) => {
      await page.route('**/api/v1/conversations/*', (route) =>
        route.request().method() === 'GET'
          ? route.fulfill({ status: 422, contentType: 'application/json', body: JSON.stringify({ error: 'The daemon could not read this conversation.' }) })
          : route.fallback(),
      );
    },
  },
  {
    // The request never answers, so the skeleton stays up (the shot waits past its 150ms delay).
    name: 'chat-loading',
    path: `/chat/${CONV_ID}`,
    ready: '.chat-skel',
    setup: async (page) => {
      await page.route('**/api/v1/conversations/*', () => new Promise(() => {}));
    },
  },
];

test.describe.configure({ mode: 'parallel' });

for (const v of VARIANTS) {
  test.describe(v.name, () => {
    test.use({
      viewport: v.viewport,
      colorScheme: v.scheme,
      deviceScaleFactor: v.dpr,
      isMobile: v.mobile,
      hasTouch: v.mobile,
    });

    for (const scene of SCENES) {
      test(scene.name, async ({ page }) => {
        // Freeze Date so relative times ("5m ago") are the same on every run.
        await page.clock.install({ time: new Date(FROZEN) });
        await mockBackend(page);
        await scene.setup(page);
        await page.goto(scene.path);
        await page.locator('main.main').waitFor();
        await page.locator(scene.ready).first().waitFor();
        if (scene.name !== 'chat-loading') await page.waitForLoadState('networkidle');
        await expect(page.getByText('Loading…')).toHaveCount(0);
        // No animations, transitions or blinking caret.
        await page.addStyleTag({
          content: '*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important}',
        });
        if (scene.open) {
          await page.getByRole('button', { name: /^Worked/ }).first().waitFor();
          for (const fold of await page.getByRole('button', { name: /^Worked/ }).all()) {
            // The task fixture lists one run twice, so two folds share a key: open each only if still shut.
            if ((await fold.getAttribute('aria-expanded')) === 'false') await fold.click();
          }
          const head = page.locator('.tl-head').first();
          if (await head.count()) await head.click();
        }
        if (scene.pick) await page.getByRole('button', { name: new RegExp(`^${scene.pick}`) }).click();
        // Threads open at the bottom; make sure of it before the shot.
        await page.evaluate(() => {
          for (const el of document.querySelectorAll<HTMLElement>('*')) {
            if (el.scrollHeight > el.clientHeight + 1 && /(auto|scroll)/.test(getComputedStyle(el).overflowY)) el.scrollTop = el.scrollHeight;
          }
        });
        if (scene.away) {
          await page.evaluate(() => {
            for (const el of document.querySelectorAll<HTMLElement>('*')) {
              if (el.scrollHeight > el.clientHeight + 1 && /(auto|scroll)/.test(getComputedStyle(el).overflowY)) el.scrollTop = 0;
            }
            window.scrollTo(0, 0);
          });
          await page.locator('.jump-latest').waitFor();
        }
        mkdirSync(OUT, { recursive: true });
        await page.screenshot({ path: `${OUT}/${scene.name}-${v.name}.png`, animations: 'disabled' });
      });
    }
  });
}
