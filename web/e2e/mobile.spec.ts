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
import { CHAT_TASK_ID, CONV_ID, GOAL_ID, RUN_ID, TASK_ID, conversations, longChatMessages } from './fixtures';
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
const KNOWN_MOBILE_FAILURES: Record<string, string[]> = {};

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

    // The top bar sits flush under the status bar in both colour schemes: its colour is the
    // theme-color Safari tints its own bar with, it stays at the top edge while the page scrolls,
    // and its top padding is the safe-area inset (faked through --topbar-h, as above).
    for (const scheme of ['light', 'dark'] as const) {
      test(`top bar matches theme-color and sits at the safe-area inset (${scheme})`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        await open(page, '/tasks');
        await page.addStyleTag({ content: ':root { --topbar-h: calc(56px + 47px) !important; }' });
        const bar = page.locator('.topbar');
        const bg = await bar.evaluate((el) => getComputedStyle(el).backgroundColor);
        const themeColor = await page.evaluate((s) => {
          const metas = [...document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')];
          const m = metas.find((x) => matchMedia(x.media || 'all').matches && x.media.includes(s));
          const probe = document.createElement('i');
          probe.style.color = m?.content ?? '';
          document.body.append(probe);
          const c = getComputedStyle(probe).color;
          probe.remove();
          return c;
        }, scheme);
        expect(bg).toBe(themeColor);
        expect(await bar.evaluate((el) => getComputedStyle(el).paddingTop)).toBe('47px');
        await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
        expect((await bar.boundingBox())?.y).toBeCloseTo(0, 0);
        mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
        await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/topbar-${scheme}.png` });
      });
    }

    // Same trick for the home indicator: the chat composer is sticky to the bottom edge, so on a
    // notched iPhone its lower ~34px sits under the indicator. --composer-inset stands in for
    // env(safe-area-inset-bottom), which Chromium reports as 0.
    // Several pending files, some with very long names: the chips wrap inside the composer, the
    // page stays no wider than the screen, and every control is at least 44px to tap.
    test('chat composer with several pending attachments fits and is tappable', async ({ page }) => {
      await open(page, `/chat/${CONV_ID}`);
      const long = 'a-very-long-file-name-that-must-be-cut-with-an-ellipsis-instead-of-pushing-the-page-wider.txt';
      const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');
      await page.getByTestId('attach-input').setInputFiles([
        { name: 'photo-1.png', mimeType: 'image/png', buffer: png },
        { name: 'photo-2.png', mimeType: 'image/png', buffer: png },
        { name: long, mimeType: 'text/plain', buffer: Buffer.from('hello') },
        { name: 'notes.md', mimeType: 'text/markdown', buffer: Buffer.from('# hi') },
      ]);
      const chips = page.locator('.composer .att-chip');
      await expect(chips).toHaveCount(4);
      await expect(page.locator('.composer progress')).toHaveCount(0);
      const vw = page.viewportSize()!.width;
      for (const box of await Promise.all((await chips.all()).map((c) => c.boundingBox()))) {
        expect(box!.x).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width).toBeLessThanOrEqual(vw);
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      for (const button of await page.locator('.composer .att-remove, .composer .icon-btn').all()) {
        const box = await button.boundingBox();
        expect(box!.width).toBeGreaterThanOrEqual(44);
        expect(box!.height).toBeGreaterThanOrEqual(44);
      }
      await expect(page.getByRole('button', { name: 'Add a photo' })).toBeVisible();
      mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
      await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/chat-composer-attachments.png` });
    });

    test('chat code blocks: no page overflow, copy and wrap are tappable', async ({ page }) => {
      await open(page, `/chat/${CONV_ID}`);
      const blocks = page.locator('.code-block');
      await expect(blocks).toHaveCount(2);
      const vw = page.viewportSize()!.width;
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      const coarse = await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
      for (const button of await page.locator('.code-block-btn').all()) {
        await button.scrollIntoViewIfNeeded();
        const box = await button.boundingBox();
        expect(box!.x + box!.width).toBeLessThanOrEqual(vw);
        if (coarse) {
          expect(box!.width).toBeGreaterThanOrEqual(44);
          expect(box!.height).toBeGreaterThanOrEqual(44);
        }
      }
      const go = blocks.nth(0);
      const goPre = go.locator('pre');
      expect(await goPre.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true);
      await go.getByRole('button', { name: /^Wrap/ }).tap();
      await expect(go.getByRole('button', { name: /^Wrap/ })).toHaveAttribute('aria-pressed', 'true');
      expect(await goPre.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      await go.scrollIntoViewIfNeeded();
      mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
      await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/chat-code-blocks.png` });
    });

    test('chat composer clears a 34px home indicator', async ({ page }) => {
      await open(page, `/chat/${CONV_ID}`);
      await page.addStyleTag({ content: ':root { --composer-inset: 34px !important; }' });
      await page.evaluate(() => {
        window.scrollTo(0, 0);
        const main = document.querySelector('main.main') as HTMLElement;
        main.scrollTop = 0;
      });
      const box = await page.locator('.chat-page .composer').boundingBox();
      const vh = page.viewportSize()!.height;
      expect(box, 'composer is rendered').not.toBeNull();
      expect(vh - (box!.y + box!.height), `composer bottom is ${box!.y + box!.height}px in a ${vh}px viewport`).toBeGreaterThanOrEqual(33.5);
    });

    // Touch devices have no hover, so the per-message copy button is always shown, 44px or more,
    // and inside the screen.
    test('chat message copy buttons are visible and tappable without hover', async ({ page }) => {
      await open(page, `/chat/${CONV_ID}`);
      const buttons = page.getByRole('button', { name: 'Copy message' });
      await expect(buttons.first()).toBeVisible();
      const vw = page.viewportSize()!.width;
      for (const b of await buttons.all()) {
        // toBeVisible ignores opacity, so check the wrapper that fades in on hover.
        expect(await b.evaluate((el) => getComputedStyle(el.parentElement!).opacity)).toBe('1');
        const box = await b.boundingBox();
        expect(box!.width).toBeGreaterThanOrEqual(44);
        expect(box!.height).toBeGreaterThanOrEqual(44);
        expect(box!.x).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width).toBeLessThanOrEqual(vw);
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
    });

    // A 5-call run (long command, edit, error): collapsed and expanded it stays inside the screen
    // and the run header and every call row are at least 44px tall on touch.
    test('chat tool run: no overflow, header and call rows are tappable', async ({ page }) => {
      await open(page, `/chat/task/${CHAT_TASK_ID}`);
      const vw = page.viewportSize()!.width;
      const head = page.getByRole('button', { name: /Ran 2 commands, read 1 file, edited 1 file, searched 1 time/ }).first();
      await head.scrollIntoViewIfNeeded();
      await expect(head).toHaveAttribute('aria-expanded', 'false');
      const coarse = await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
      const check = async (button: ReturnType<Page['locator']>) => {
        const box = await button.boundingBox();
        expect(box!.x).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width).toBeLessThanOrEqual(vw);
        if (coarse) expect(box!.height).toBeGreaterThanOrEqual(44);
      };
      await check(head);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      await head.tap();
      await expect(head).toHaveAttribute('aria-expanded', 'true');
      const rows = page.locator('.tl-calls').first().locator('.tl-row');
      await expect(rows).toHaveCount(5);
      for (const row of await rows.all()) await check(row);
      await rows.nth(4).tap();
      await expect(rows.nth(4)).toHaveAttribute('aria-expanded', 'true');
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
      await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/chat-tool-run.png` });
    });

    // A long chat opens at the end. Scrolling up shows the Jump to latest pill: inside the screen,
    // clear of the composer, 44px tall; tapping it returns to the end and hides it.
    test('chat Jump to latest pill is visible, tappable and returns to the bottom', async ({ page }) => {
      // Same steps as open(), with the long conversation routed after the mock so it wins.
      await mockBackend(page);
      await page.route(`**/api/v1/conversations/${CONV_ID}`, (route) =>
        route.fulfill({ json: { conversation: conversations[0], messages: longChatMessages } }),
      );
      await page.goto(`/chat/${CONV_ID}`);
      await page.locator('main.main').waitFor();
      await page.waitForLoadState('networkidle');
      const distanceFromEnd = () =>
        page.evaluate(() => {
          const r = document.documentElement;
          return r.scrollHeight - r.scrollTop - window.innerHeight;
        });
      await expect.poll(distanceFromEnd).toBeLessThan(2);
      const pill = page.getByRole('button', { name: /jump to latest/i });
      await expect(pill).toHaveCount(0);

      await page.evaluate(() => window.scrollTo(0, 0));
      await expect(pill).toBeVisible();
      const box = (await pill.boundingBox())!;
      const composer = (await page.locator('.chat-page .composer').boundingBox())!;
      const vw = page.viewportSize()!.width;
      expect(box.height).toBeGreaterThanOrEqual(44);
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(vw);
      expect(box.y + box.height).toBeLessThanOrEqual(composer.y);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
      await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/chat-jump-to-latest.png` });

      await pill.tap();
      await expect.poll(distanceFromEnd).toBeLessThan(2);
      await expect(pill).toHaveCount(0);
    });

    // A long draft stops growing at 40% of the screen and scrolls inside the field, so the
    // composer stays on screen; Send and Attach stay 44px tappable.
    test('chat composer caps a 30-line draft and keeps Send tappable', async ({ page }) => {
      await open(page, `/chat/${CONV_ID}`);
      await page.addStyleTag({ content: ':root { --composer-inset: 34px !important; }' });
      const field = page.getByLabel('Message the orchestrator');
      await field.fill(Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join('\n'));
      const vh = page.viewportSize()!.height;
      const vw = page.viewportSize()!.width;
      const fieldBox = (await field.boundingBox())!;
      expect(fieldBox.height).toBeLessThanOrEqual(vh * 0.4 + 1);
      expect(fieldBox.height, 'grows to the cap, not one line').toBeGreaterThanOrEqual(vh * 0.4 - 2);
      expect(await field.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
      const box = (await page.locator('.chat-page .composer').boundingBox())!;
      expect(box.y).toBeGreaterThanOrEqual(0);
      expect(vh - (box.y + box.height)).toBeGreaterThanOrEqual(33.5);
      for (const name of ['Send', 'Attach files']) {
        const b = (await page.getByRole('button', { name, exact: true }).boundingBox())!;
        expect(b.width, name).toBeGreaterThanOrEqual(44);
        expect(b.height, name).toBeGreaterThanOrEqual(44);
        expect(b.x + b.width, name).toBeLessThanOrEqual(vw);
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
      await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/chat-composer-long-draft.png` });
    });

    test('chat activity line: fits without overflow, link is tappable', async ({ page }) => {
      const vw = page.viewportSize()!.width;
      // The mock's task thread has finished its last turn: "Worked for ...". The live wording is
      // covered by ActivityLine.test.tsx.
      await open(page, `/chat/task/${CHAT_TASK_ID}`);
      const line = page.locator('.chat-page .activity-line');
      await line.scrollIntoViewIfNeeded();
      await expect(line).toContainText(/Worked for/);
      const box = await line.boundingBox();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(vw);
      expect(box!.height).toBeLessThanOrEqual(56);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(vw);
      mkdirSync(`e2e-results/screenshots/${vp.name}`, { recursive: true });
      await page.screenshot({ path: `e2e-results/screenshots/${vp.name}/chat-activity.png` });
    });
  });
}
