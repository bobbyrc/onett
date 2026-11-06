#!/usr/bin/env node
// Read-only screenshots of the real UI served by a running daemon (docs/ui.md, "Screenshots for
// UI PRs"). Unlike `npm run shots:chat`, which mocks the backend, this shows real data.
//
//   ONETT_API=http://127.0.0.1:7777 ONETT_TOKEN=... node scripts/shoot-live.mjs [options]
//
// Options:
//   --routes /a,/b   routes to shoot (default: every route in src/App.tsx plus one detail page each)
//   --out DIR        where the PNGs go (default e2e-results/live-shots)
//   --full           capture the whole scrolling page instead of the first screen
//
// Safe by construction: every request that is not a GET is aborted in the browser, so the script
// can never change anything. The token is sent as `Authorization: Bearer` on API calls only and
// is never printed. A token is not a session, so GET /api/v1/auth/state is answered locally with
// a signed-in owner, which makes the UI render its signed-in pages. The token still cannot read
// owner-only data (config, users, agents): those panels show "forbidden", and the script says so
// per route instead of leaving it to look like a bug. Onett deliberately has no read-only owner
// session for this: the API token must never gain owner powers (AGENTS.md, invariant 12).
import { mkdirSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const API = (process.env.ONETT_API ?? '').replace(/\/+$/, '');
const TOKEN = process.env.ONETT_TOKEN ?? '';
if (!API || !TOKEN) {
  console.error('set ONETT_API (for example http://127.0.0.1:7777) and ONETT_TOKEN');
  process.exit(2);
}

const args = process.argv.slice(2);
const opt = (name) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
};
const outDir = resolve(opt('--out') ?? join(webDir, 'e2e-results/live-shots'));
const fullPage = args.includes('--full');

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'phone', width: 390, height: 844 },
];
const SCHEMES = ['dark', 'light'];

async function get(path) {
  const res = await fetch(`${API}/api/v1${path}`, { headers: { Authorization: `Bearer ${TOKEN}` } });
  if (!res.ok) throw new Error(`GET ${path}: ${res.status}`);
  return res.json();
}

// The routes the app declares, minus catch-alls and plain redirects.
function appRoutes() {
  const src = readFileSync(join(webDir, 'src/App.tsx'), 'utf8');
  const out = [];
  for (const line of src.split('\n')) {
    const m = line.match(/<Route path="([^"]+)"/);
    if (m && m[1] !== '*' && !line.includes('<Navigate')) out.push(m[1]);
  }
  return out;
}

// Fills each :param route with a real id from the API; a route with no data to show is skipped.
async function defaultRoutes() {
  const first = async (path) => {
    try {
      const list = await get(path);
      return Array.isArray(list) && list.length > 0 ? list[0].id : undefined;
    } catch (err) {
      console.error(`note: ${err.message}`);
      return undefined;
    }
  };
  const ids = {
    '/chat/task/:taskId': await first('/tasks'),
    '/chat/:id': await first('/conversations'),
    '/goals/:id': await first('/goals'),
    '/tasks/:id': await first('/tasks'),
    '/runs/:id': await first('/runs'),
  };
  const routes = [];
  // App.tsx guards /users with a redirect on the same line, so appRoutes() skips it.
  for (const r of [...appRoutes(), '/users']) {
    if (!r.includes(':')) routes.push(r);
    else if (ids[r]) routes.push(r.replace(/:\w+/, ids[r]));
    else console.error(`note: no data for ${r}, skipped`);
  }
  return routes;
}

const routes = opt('--routes')?.split(',').filter(Boolean) ?? (await defaultRoutes());
mkdirSync(outDir, { recursive: true });

const owner = {
  setup_required: false,
  user: { username: 'owner', role: 'owner', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
};

const browser = await chromium.launch();
let shots = 0;
const forbidden = new Map(); // route -> API paths the token was refused
try {
  for (const colorScheme of SCHEMES) {
    for (const vp of VIEWPORTS) {
      const context = await browser.newContext({ viewport: { width: vp.width, height: vp.height }, colorScheme });
      await context.route('**/*', async (route) => {
        const req = route.request();
        if (req.method() !== 'GET') return route.abort();
        const url = new URL(req.url());
        if (url.origin !== new URL(API).origin) return route.continue();
        if (url.pathname === '/api/v1/auth/state') {
          return route.fulfill({ json: owner });
        }
        if (url.pathname.startsWith('/api/')) {
          return route.continue({ headers: { ...req.headers(), authorization: `Bearer ${TOKEN}` } });
        }
        return route.continue();
      });
      const page = await context.newPage();
      let current = '';
      page.on('response', (res) => {
        const url = new URL(res.url());
        if (res.status() !== 403 || !url.pathname.startsWith('/api/')) return;
        const seen = forbidden.get(current) ?? new Set();
        seen.add(url.pathname);
        forbidden.set(current, seen);
      });
      for (const route of routes) {
        current = route;
        await page.goto(API + route, { waitUntil: 'domcontentloaded' });
        // The event stream keeps the network busy, so idle may never come; give it a few seconds.
        await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => {});
        await page.waitForTimeout(500);
        const name = (route === '/' ? 'overview' : route.replace(/^\//, '').replace(/[^\w-]+/g, '_')).slice(0, 80);
        const file = join(outDir, `${name}-${vp.name}-${colorScheme}.png`);
        await page.screenshot({ path: file, fullPage });
        shots++;
      }
      await context.close();
    }
  }
} finally {
  await browser.close();
}
for (const [route, paths] of forbidden) {
  console.error(`note: ${route}: owner-only data not shown, the token got 403 from ${[...paths].join(', ')}`);
}
console.log(`wrote ${shots} PNGs for ${routes.length} routes to ${outDir}`);
