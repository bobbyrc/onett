// Screenshots every mockup state into shots/. Run from the repository root:
//   node docs/proposals/triggers-ui/shoot.mjs
// It needs `cd web && npm ci` and Playwright's Chromium
// (`cd web && npx playwright install chromium-headless-shell`). SHOTS_SCHEME=dark and
// SHOTS_OUT=<dir> shoot the dark scheme somewhere else, for a quick check.
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(here, '../../../web/package.json'));
const { chromium } = require('@playwright/test');

const scheme = process.env.SHOTS_SCHEME ?? 'light';
const out = process.env.SHOTS_OUT ?? path.join(here, 'shots');
mkdirSync(out, { recursive: true });

const STATES = ['list', 'empty', 'form-schedule', 'form-issue', 'history', 'readonly'];
const SIZES = { desktop: { width: 1440, height: 900 }, phone: { width: 390, height: 844 } };

const jobs = [];
for (const direction of ['a', 'b']) {
  for (const state of STATES) jobs.push({ file: `${direction}-${state}.html`, name: `${direction}-${state}`, sizes: ['desktop', 'phone'] });
}
// Direction B's phone layout has two tabs; this is the second one.
jobs.push({ file: 'b-list.html', hash: '#firings', name: 'b-list-firings', sizes: ['phone'] });

const browser = await chromium.launch({ args: ['--force-color-profile=srgb'] });
const problems = [];
for (const job of jobs) {
  for (const size of job.sizes) {
    const context = await browser.newContext({ viewport: SIZES[size], colorScheme: scheme, reducedMotion: 'reduce', deviceScaleFactor: 1 });
    const page = await context.newPage();
    await page.goto(pathToFileURL(path.join(here, 'mockups', job.file)).href + (job.hash ?? ''));
    await page.evaluate(() => document.fonts.ready);
    const info = await page.evaluate(() => {
      const modal = document.querySelector('.modal');
      return {
        css: getComputedStyle(document.body).fontFamily.includes('Barlow'),
        fonts: document.fonts.check("15px 'Barlow'") && document.fonts.check("700 30px 'Barlow Condensed'"),
        shell: !!document.querySelector('.sidebar'),
        wide: document.documentElement.scrollWidth > window.innerWidth,
        tall: document.documentElement.scrollHeight > window.innerHeight + 1,
        modal: !!modal,
        modalTall: !!modal && modal.scrollHeight > modal.clientHeight + 1,
      };
    });
    const base = `${job.name}-${size}`;
    if (!info.css) problems.push(`${base}: web/src/styles.css did not load`);
    if (!info.fonts) problems.push(`${base}: Barlow did not load`);
    if (!info.shell) problems.push(`${base}: shell.js did not draw the rail`);
    if (info.wide) problems.push(`${base}: the page scrolls sideways`);
    await page.screenshot({ path: path.join(out, `${base}.png`) });
    // A page longer than the window gets a second, whole-page shot, so the cut at the fold is
    // not mistaken for a bug. A dialog scrolls inside itself, so its second shot is its end.
    if (info.tall && !info.modal) await page.screenshot({ path: path.join(out, `${base}-full.png`), fullPage: true });
    if (info.modalTall) {
      await page.evaluate(() => {
        const modal = document.querySelector('.modal');
        modal.scrollTop = modal.scrollHeight;
      });
      await page.screenshot({ path: path.join(out, `${base}-bottom.png`) });
    }
    await context.close();
    console.log(base);
  }
}
await browser.close();
if (problems.length > 0) {
  console.error(problems.join('\n'));
  process.exit(1);
}
