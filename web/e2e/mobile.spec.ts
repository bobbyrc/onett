// Mobile layout check: every route at two phone sizes, against a fully mocked backend.
//
//   overflow  the page is no wider than the viewport (no sideways scrolling)
//   content   <main> starts inside the viewport at the top and ends inside it at the bottom
//   nav       with the menu open every nav item is the topmost element at its centre
//             (once per viewport: the shell is shared)
//
// Run with `npm run test:mobile`. Screenshots of every route land in e2e-results/screenshots.
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { CHAT_TASK_ID, CONV_ID, GOAL_ID, RUN_ID, TASK_ID } from './fixtures';
import { mockBackend } from './mock';

const VIEWPORTS = [
  { name: 'iphone-390x844', width: 390, height: 844, isMobile: true, hasTouch: true, deviceScaleFactor: 2 },
  { name: 'android-360x800', width: 360, height: 800, isMobile: true, hasTouch: true, deviceScaleFactor: 2 },
] as const;

const ROUTES = [
  '/',
  '/projects',
  '/chat',
  `/chat/${CONV_ID}`,
  `/chat/task/${CHAT_TASK_ID}`,
  '/goals',
  `/goals/${GOAL_ID}`,
  '/tasks',
  `/tasks/${TASK_ID}`,
  '/runs',
  `/runs/${RUN_ID}`,
  '/approvals',
  '/deploys',
  '/signals',
  '/friction',
  '/loops',
  '/agents',
  '/config',
  '/users',
];

type Check = 'overflow' | 'content' | 'nav';

// The one list of known mobile bugs. Each route/assertion pair here is skipped with test.fixme
// (not test.fail), so a parallel fix that makes one pass never breaks CI. Remove the entry in
// the same PR that fixes the bug. Keys are ROUTES entries; a key may also list a viewport name
// to limit it, as 'overflow@android-360x800'.
// Reported by the first run; every one is the unbroken-title fixture (or a long unbroken value)
// forcing the page wider than the phone.
const KNOWN_MOBILE_FAILURES: Record<string, string[]> = {
  '/': ['overflow'], // callout p, approval-card h3 and blocked call-item h3/call-body: ~1700px of unbroken title
  [`/goals`]: ['overflow'], // table td holding the unbroken goal-title link
  [`/goals/${GOAL_ID}`]: ['overflow'], // table td holding the unbroken task-title link
  '/tasks': ['overflow'], // table td with the unbroken title link; div.muted.small, span.tag
  [`/tasks/${TASK_ID}`]: ['overflow'], // div.task-body (unbroken body text), ol.task-line station label, subtask link
  '/approvals': ['overflow'], // article.approval-card div.call-title h3 (unbroken title)
  '/signals': ['overflow'], // table td with the unbroken title link
  '/friction': ['overflow'], // table td with the unbroken title
};

const isKnown = (route: string, check: Check, viewport: string) => {
  const entries = KNOWN_MOBILE_FAILURES[route] ?? [];
  return entries.includes(check) || entries.includes(`${check}@${viewport}`);
};

/** Opens a route and waits until it has rendered and its data has loaded. */
async function open(page: Page, route: string) {
  await mockBackend(page);
  await page.goto(route);
  await page.locator('main.main').waitFor();
  await page.waitForLoadState('networkidle');
  await expect(page.getByText('Loading…')).toHaveCount(0);
  await page.evaluate(() => document.fonts.ready);
}

const slug = (route: string) => (route === '/' ? 'overview' : route.slice(1).replace(/[^a-zA-Z0-9]+/g, '-'));

/** Opens the drawer and polls until every nav item is the topmost element at its centre. */
async function expectNavReachable(page: Page) {
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(page.locator('#sidebar')).toHaveClass(/open/);
  const items = page.locator('nav[aria-label="Main"] .nav-item');
  expect(await items.count()).toBeGreaterThan(10);
  // Poll: the drawer slides in, so the first reads may still be mid-transition.
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const blocked: string[] = [];
          for (const el of Array.from(document.querySelectorAll('nav[aria-label="Main"] .nav-item'))) {
            const r = el.getBoundingClientRect();
            const cx = r.left + r.width / 2;
            const cy = r.top + r.height / 2;
            const top = document.elementFromPoint(cx, cy);
            if (!top || !(top === el || el.contains(top))) {
              const t = top ? `${top.tagName.toLowerCase()}.${String(top.className)}` : 'nothing';
              blocked.push(`"${el.textContent?.trim()}" at (${Math.round(cx)},${Math.round(cy)}) is covered by ${t}`);
            }
          }
          return blocked;
        }),
      { timeout: 5_000 },
    )
    .toEqual([]);
}

for (const vp of VIEWPORTS) {
  test.describe(vp.name, () => {
    test.use({
      viewport: { width: vp.width, height: vp.height },
      isMobile: vp.isMobile,
      hasTouch: vp.hasTouch,
      deviceScaleFactor: vp.deviceScaleFactor,
    });

    for (const route of ROUTES) {
      test.describe(route, () => {
        test('screenshot', async ({ page }) => {
          await open(page, route);
          mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
          await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/${slug(route)}.png`, fullPage: true });
        });

        test('no horizontal overflow', async ({ page }) => {
          test.fixme(isKnown(route, 'overflow', vp.name), 'known mobile bug: see KNOWN_MOBILE_FAILURES');
          await open(page, route);
          const result = await page.evaluate(() => {
            const root = document.documentElement;
            const vw = root.clientWidth;
            const name = (el: Element) => {
              const cls = typeof el.className === 'string' && el.className ? `.${el.className.trim().split(/\s+/).join('.')}` : '';
              return `${el.tagName.toLowerCase()}${el.id ? `#${el.id}` : ''}${cls}`;
            };
            const all = Array.from(document.body.querySelectorAll('*'));
            // Elements whose box sticks out past the right edge...
            const offenders = all
              .map((el) => ({ el, r: el.getBoundingClientRect() }))
              .filter(({ r }) => r.width > 0 && r.right > vw + 0.5)
              .slice(0, 12)
              .map(({ el, r }) => `${name(el)} (right=${Math.round(r.right)})`);
            // ...and boxes inside the viewport whose own content (unbroken text) spills out.
            for (const el of all) {
              if (offenders.length >= 16) break;
              if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0 && el.getBoundingClientRect().right <= vw + 0.5) {
                offenders.push(`${name(el)} (content ${el.scrollWidth}px in a ${el.clientWidth}px box)`);
              }
            }
            return { scrollWidth: root.scrollWidth, clientWidth: vw, offenders };
          });
          expect(
            result.scrollWidth,
            `page is ${result.scrollWidth}px wide in a ${result.clientWidth}px viewport. Elements past the right edge:\n  ${result.offenders.join('\n  ')}`,
          ).toBeLessThanOrEqual(result.clientWidth);
        });

        test('main content stays in the viewport', async ({ page }) => {
          test.fixme(isKnown(route, 'content', vp.name), 'known mobile bug: see KNOWN_MOBILE_FAILURES');
          await open(page, route);
          const measure = (where: 'top' | 'end') =>
            page.evaluate((w) => {
              const main = document.querySelector('main.main') as HTMLElement;
              if (w === 'top') {
                window.scrollTo(0, 0);
                main.scrollTop = 0;
              } else {
                window.scrollTo(0, document.documentElement.scrollHeight);
                main.scrollTop = main.scrollHeight;
              }
              const r = main.getBoundingClientRect();
              return { top: r.top, bottom: r.bottom, vh: window.innerHeight };
            }, where);
          const top = await measure('top');
          expect(top.top, `main top is ${top.top}px at scroll top (viewport ${top.vh}px high)`).toBeGreaterThanOrEqual(-0.5);
          expect(top.top, `main top is ${top.top}px at scroll top (viewport ${top.vh}px high)`).toBeLessThan(top.vh);
          const end = await measure('end');
          expect(end.bottom, `main bottom is ${end.bottom}px at scroll end (viewport ${end.vh}px high)`).toBeGreaterThan(0);
          expect(end.bottom, `main bottom is ${end.bottom}px at scroll end (viewport ${end.vh}px high)`).toBeLessThanOrEqual(end.vh + 0.5);
        });
      });
    }

    test('mobile menu: every nav item is reachable', async ({ page }) => {
      test.fixme(isKnown('menu', 'nav', vp.name), 'known mobile bug: see KNOWN_MOBILE_FAILURES');
      await open(page, '/');
      await expectNavReachable(page);
    });

    // Chromium reports env(safe-area-inset-top) as 0, so the plain check above cannot see the
    // notch bug (the top bar growing taller than the drawer's offset). Setting --topbar-h to what
    // a 47px notch gives makes the real top bar that tall; a drawer with a fixed offset would
    // start under the bar and cover the first item.
    test('mobile menu: every nav item is reachable under a 47px notch', async ({ page }) => {
      await open(page, '/');
      await page.addStyleTag({ content: ':root { --topbar-h: calc(56px + 47px) !important; }' });
      const bar = await page.locator('.topbar').boundingBox();
      expect(bar?.height).toBeCloseTo(103, 0);
      await expectNavReachable(page);
      const side = await page.locator('#sidebar').boundingBox();
      expect(side?.y).toBeCloseTo(103, 0);
    });
  });
}
