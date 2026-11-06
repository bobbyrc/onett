// Screenshots of every page, with realistic mocked data, for PR descriptions (npm run shots). It
// shoots each route in src/App.tsx (plus a detail page per :param route) as the signed-in owner,
// at desktop and phone width in light and dark, into
// e2e-results/shots/<route>-<desktop|phone>-<dark|light>.png (the naming of scripts/shoot-live.mjs,
// which needs a daemon and a token that worker agents do not have). Data: e2e/page-fixtures.ts.
// The clock is frozen and animations are off, so two runs of one commit give identical PNGs.
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { mockBackend } from './mock';
import * as d from './page-fixtures';

const OUT = 'e2e-results/shots';

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 900, mobile: false },
  { name: 'phone', width: 390, height: 844, mobile: true },
] as const;
const SCHEMES = ['dark', 'light'] as const;

type Variant = 'default' | 'emptyTasks' | 'emptyRuns' | 'oneProject';

/** The fixture body for a GET path (without /api/v1), or undefined to fall back to mock.ts. */
function respond(path: string, variant: Variant, query: URLSearchParams): unknown {
  // The pages ask for lists by status ("starting,running"); honour it like the daemon does.
  const byStatus = <T extends { status: string }>(rows: T[]) => {
    const want = query.get('status')?.split(',').filter(Boolean);
    return want?.length ? rows.filter((r) => want.includes(r.status)) : rows;
  };
  const [head, a, b] = path.split('/').filter(Boolean);
  switch (head) {
    case 'auth':
      return a === 'state' ? { setup_required: false, user: d.owner } : undefined;
    case 'stats':
      return d.stats;
    case 'users':
      return d.users;
    case 'secrets':
      return { secrets: d.secrets };
    case 'projects':
      return variant === 'oneProject' ? d.oneProject : d.projects;
    case 'tasks':
      if (!a) return variant === 'emptyTasks' ? [] : byStatus(d.tasks);
      if (b === 'conversation') return d.taskConversation;
      return {
        task: d.tasks.find((t) => t.id === a) ?? d.tasks[0],
        runs: d.runs.filter((r) => r.task_id === a),
        approvals: d.approvals.filter((x) => x.task_id === a),
        children: a === d.IDS.task ? d.tasks.slice(6, 8) : [],
      };
    case 'goals':
      return a
        ? { goal: d.goals.find((g) => g.id === a) ?? d.goals[0], tasks: d.tasks.filter((t) => t.goal_id === a) }
        : d.goals;
    case 'runs':
      if (!a) return variant === 'emptyRuns' ? [] : byStatus(d.runs);
      if (b === 'logs') return a === d.IDS.chatRun ? d.chatRunLogs : a === d.IDS.goalRun ? d.goalRunLogs : d.runLogs;
      return d.runs.find((r) => r.id === a) ?? d.runs[0];
    case 'approvals':
      return byStatus(d.approvals);
    case 'deployments':
      return d.deployments;
    case 'signals':
      return d.signals;
    case 'friction':
      return d.friction;
    case 'loops':
      return d.loops;
    case 'agents':
      return d.agents;
    case 'agent-conversations':
      return [{ task_id: d.IDS.chatTask, title: d.tasks[1].title, project: 'onett', role: 'senior-engineer', task_status: 'running', live: true, closed: false, updated_at: d.conversations[5].updated_at }];
    case 'conversations':
      if (!a) return d.conversations;
      if (b) return undefined;
      return {
        conversation: d.conversations.find((c) => c.id === a) ?? d.conversations[0],
        messages: a === d.IDS.goalChat ? d.goalChatMessages : d.chatMessages,
      };
    case 'events':
      return d.events;
    case 'config':
      if (a === 'document') return d.configDocument;
      if (a === 'file') return d.configText;
      return d.config;
    default:
      return undefined;
  }
}

// A small picture for the chat's image attachment (the shared mock serves a 1x1 PNG).
const SCREENSHOT_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="400"><rect width="640" height="400" fill="#eef1f6"/>' +
  '<rect width="640" height="36" fill="#2b3a55"/><g fill="#8aa0c8"><rect x="24" y="64" width="592" height="40" rx="6"/>' +
  '<rect x="24" y="120" width="592" height="40" rx="6"/><rect x="24" y="176" width="592" height="40" rx="6"/>' +
  '<rect x="24" y="232" width="400" height="40" rx="6"/></g></svg>';

async function install(page: Page, variant: Variant) {
  await mockBackend(page);
  // The stream stays "reconnecting…" until a chat or run watch is confirmed, so answer subscribes.
  await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
    ws.onMessage((raw) => {
      const msg = JSON.parse(String(raw)) as { kind: string; run_id?: string; conversation_id?: string };
      if (msg.kind === 'subscribe') ws.send(JSON.stringify({ kind: 'subscribed', run_id: msg.run_id, conversation_id: msg.conversation_id }));
    });
  });
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace(/^\/api\/v1/, '');
    if (req.method() !== 'GET') return route.fallback();
    if (/^\/conversations\/[^/]+\/attachments\//.test(path)) {
      return route.fulfill({ status: 200, contentType: 'image/svg+xml', body: SCREENSHOT_SVG });
    }
    const body = respond(path, variant, url.searchParams);
    if (body === undefined) return route.fallback();
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
}

// The routes App.tsx declares, minus catch-alls and plain redirects (as scripts/shoot-live.mjs does).
function appRoutes(): string[] {
  const out: string[] = [];
  for (const line of readFileSync('src/App.tsx', 'utf8').split('\n')) {
    const m = line.match(/<Route path="([^"]+)"/);
    if (m && m[1] !== '*' && !line.includes('<Navigate')) out.push(m[1]);
  }
  return out;
}

const PARAMS: Record<string, string> = {
  '/chat/task/:taskId': d.IDS.chatTask,
  '/chat/:id': d.IDS.chat,
  '/goals/:id': d.IDS.goal,
  '/tasks/:id': d.IDS.task,
  '/runs/:id': d.IDS.run,
};

type Scene = { route: string; variant: Variant; name?: string };

const SCENES: Scene[] = [
  ...appRoutes().map((r) => {
    const route = r.includes(':') ? PARAMS[r] && r.replace(/:\w+/, PARAMS[r]) : r;
    if (!route) throw new Error(`page-shots: no fixture id for route ${r}; add it to PARAMS`);
    return { route, variant: 'default' as Variant };
  }),
  // App.tsx guards /users with a redirect on the same line, so the scan above skips it.
  { route: '/users', variant: 'default' },
  // Extra shots for data shapes and empty states the plain routes do not show.
  { route: `/runs/${d.IDS.chatRun}`, variant: 'default' },
  { route: `/chat/${d.IDS.goalChat}`, variant: 'default' },
  { route: `/goals/${d.IDS.doneGoal}`, variant: 'default' },
  { route: '/', variant: 'oneProject', name: 'overview_one_project' },
  { route: '/tasks', variant: 'emptyTasks', name: 'tasks_empty' },
  { route: '/runs', variant: 'emptyRuns', name: 'runs_empty' },
];

const fileName = (s: Scene) =>
  s.name ?? (s.route === '/' ? 'overview' : s.route.replace(/^\//, '').replace(/[^\w-]+/g, '_')).slice(0, 80);

test.describe.configure({ mode: 'parallel' });

for (const vp of VIEWPORTS) {
  for (const scheme of SCHEMES) {
    test.describe(`${vp.name}-${scheme}`, () => {
      test.use({
        viewport: { width: vp.width, height: vp.height },
        colorScheme: scheme,
        deviceScaleFactor: 1,
        timezoneId: 'UTC',
        locale: 'en-US',
        isMobile: vp.mobile,
        hasTouch: vp.mobile,
      });

      for (const scene of SCENES) {
        test(fileName(scene), async ({ page }) => {
          await page.clock.setFixedTime(new Date(d.FROZEN));
          await install(page, scene.variant);
          await page.goto(scene.route);
          await page.locator('main.main').waitFor();
          // The sidebar's connection light starts on "reconnecting…" until the mocked socket opens.
          await page.locator('.live.live-on').first().waitFor({ state: 'attached' });
          // Chat threads fetch each run's log after the conversation loads, so settle twice.
          await page.waitForLoadState('networkidle');
          await page.waitForTimeout(400);
          await page.waitForLoadState('networkidle');
          // Fully loaded: no "Loading…" text and no chat skeleton.
          await expect(page.getByText(/Loading/)).toHaveCount(0);
          await expect(page.locator('.chat-skel')).toHaveCount(0);
          await page.addStyleTag({
            content: '*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important;scroll-behavior:auto!important}',
          });
          // Chat threads open at the bottom; make sure of it before the shot.
          if (scene.route.startsWith('/chat')) {
            await page.evaluate(() => {
              for (const el of document.querySelectorAll<HTMLElement>('*')) {
                if (el.scrollHeight > el.clientHeight + 1 && /(auto|scroll)/.test(getComputedStyle(el).overflowY)) el.scrollTop = el.scrollHeight;
              }
            });
            await page.waitForTimeout(300);
          }
          // Let images (the chat attachment) finish decoding.
          await page.waitForFunction(() => [...document.images].every((i) => i.complete));
          mkdirSync(OUT, { recursive: true });
          // A few pixels of anti-aliasing can still settle after the first paint, so shoot until
          // two consecutive frames are byte-identical.
          const shoot = () => page.screenshot({ animations: 'disabled' });
          let png = await shoot();
          for (let i = 0; i < 8; i++) {
            await page.waitForTimeout(150);
            const next = await shoot();
            if (next.equals(png)) break;
            png = next;
          }
          writeFileSync(`${OUT}/${fileName(scene)}-${vp.name}-${scheme}.png`, png);
        });
      }
    });
  }
}
