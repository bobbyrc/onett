// Screenshots every mockup state into shots/. Run from the repository root:
//   node docs/proposals/add-project/shoot.mjs [a|b|cli]
// It needs `cd web && npm ci` and Playwright's Chromium (PLAYWRIGHT_BROWSERS_PATH may point at a
// temp dir). A dialog that scrolls inside itself gets a second shot of its end (-bottom); a page
// longer than the window gets a whole-page shot (-full).
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(here, '../../../web/package.json'));
const { chromium } = require('@playwright/test');

const out = path.join(here, 'shots');
mkdirSync(out, { recursive: true });
const SIZES = { desktop: { width: 1440, height: 900 }, phone: { width: 390, height: 844 } };
const STATES = {
  a: ['pick', 'review', 'advanced', 'ready', 'signed-out', 'no-guess', 'clone-failed'],
  b: ['add', 'checklist', 'ready', 'signed-out', 'no-guess', 'clone-failed'],
};
const only = process.argv[2];

const jobs = [];
for (const [dir, states] of Object.entries(STATES)) {
  if (only && only !== dir) continue;
  for (const state of states) jobs.push({ file: `${dir}.html`, hash: state, name: `${dir}-${state}` });
}
if (!only || only === 'cli') for (const dir of ['a', 'b']) jobs.push({ file: `${dir}-cli.html`, name: `${dir}-cli` });

const browser = await chromium.launch({ args: ['--force-color-profile=srgb'] });
const problems = [];
for (const job of jobs) {
  for (const size of Object.keys(SIZES)) {
    const context = await browser.newContext({ viewport: SIZES[size], colorScheme: 'light', reducedMotion: 'reduce' });
    const page = await context.newPage();
    await page.goto(pathToFileURL(path.join(here, 'mockups', job.file)).href + (job.hash ? `#${job.hash}` : ''));
    await page.evaluate(() => document.fonts.ready);
    const info = await page.evaluate(() => {
      const modal = document.querySelector('.modal');
      return {
        css: getComputedStyle(document.body).fontFamily.includes('Barlow'),
        wide: document.documentElement.scrollWidth > window.innerWidth,
        tall: document.documentElement.scrollHeight > window.innerHeight + 1,
        modalTall: !!modal && modal.scrollHeight > modal.clientHeight + 1,
        modal: !!modal,
      };
    });
    const base = `${job.name}-${size}`;
    if (!info.css) problems.push(`${base}: web/src/styles.css did not load`);
    if (info.wide) problems.push(`${base}: the page scrolls sideways`);
    await page.screenshot({ path: path.join(out, `${base}.png`) });
    if (info.tall && !info.modal) await page.screenshot({ path: path.join(out, `${base}-full.png`), fullPage: true });
    if (info.modalTall) {
      await page.evaluate(() => {
        const modal = document.querySelector('.modal');
        modal.scrollTop = modal.scrollHeight;
      });
      await page.screenshot({ path: path.join(out, `${base}-bottom.png`) });
    }
    await context.close();
    console.log(base, info.modalTall ? '(+bottom)' : info.tall && !info.modal ? '(+full)' : '');
  }
}
await browser.close();
if (problems.length > 0) {
  console.error(problems.join('\n'));
  process.exit(1);
}
