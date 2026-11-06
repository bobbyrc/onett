// The bar above an open chat thread on a phone: a back link to the conversation list and the open
// conversation's title, ellipsed inside the screen rather than cut off at its edge. On a desktop
// the bar is hidden and the full list shows. Runs with `npm run test:mobile`.
import { expect, test, type Page } from '@playwright/test';
import { CHAT_TASK_ID, CONV_ID } from './fixtures';
import { mockBackend } from './mock';

const THREADS = [`/chat/${CONV_ID}`, `/chat/task/${CHAT_TASK_ID}`];

async function open(page: Page, route: string) {
  await mockBackend(page);
  await page.goto(route);
  await page.locator('.chat-page .composer').waitFor();
  await page.waitForLoadState('networkidle');
  await page.evaluate(() => document.fonts.ready);
}

test.describe('phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });

  for (const route of THREADS) {
    test(`${route}: back link and an ellipsed title fit the screen`, async ({ page }) => {
      await open(page, route);
      const back = page.getByRole('link', { name: 'Conversations' });
      await expect(back).toBeVisible();
      await expect(back).toHaveAttribute('href', '/chat');
      // The list rows and New conversation give way to the bar.
      await expect(page.locator('.conv-list .conv-item:visible, .conv-list .conv-agent:visible, .conv-new:visible')).toHaveCount(0);

      const title = page.locator('.conv-crumb-title');
      await expect(title).toBeVisible();
      const m = await title.evaluate((el) => {
        const r = el.getBoundingClientRect();
        return {
          right: r.right,
          vw: document.documentElement.clientWidth,
          clipped: el.scrollWidth > el.clientWidth,
          overflow: getComputedStyle(el).textOverflow,
        };
      });
      // The fixture title is long: it is cut with an ellipsis, on one line, inside the screen.
      expect(m.clipped).toBe(true);
      expect(m.overflow).toBe('ellipsis');
      expect(m.right).toBeLessThanOrEqual(m.vw);

      const box = await back.boundingBox();
      expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
    });
  }

  test('the back link opens the full list', async ({ page }) => {
    await open(page, `/chat/${CONV_ID}`);
    await page.getByRole('link', { name: 'Conversations' }).click();
    await expect(page).toHaveURL(/\/chat$/);
    await expect(page.locator('.conv-list .conv-item').first()).toBeVisible();
  });
});

test.describe('desktop', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test('the bar is hidden and the list shows', async ({ page }) => {
    await open(page, `/chat/${CONV_ID}`);
    await expect(page.locator('.conv-crumb')).toBeHidden();
    await expect(page.locator('.conv-list .conv-item').first()).toBeVisible();
  });
});
