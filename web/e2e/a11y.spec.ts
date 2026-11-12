// Keyboard and screen-reader checks in a real browser, against the mocked backend (part of
// `npm run test:mobile`): a dialog holds keyboard focus, a field's hint is its description, each
// page names its tab, the first Tab stop skips the navigation, and small controls take a tap.
import { expect, test, type Locator } from '@playwright/test';
import { RUN_ID, TASK_ID } from './fixtures';
import { mockBackend } from './mock';

test.beforeEach(async ({ page }) => {
  await mockBackend(page);
});

test.describe('keyboard, desktop', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('a dialog takes focus, keeps Tab inside, and gives focus back', async ({ page }) => {
    await page.goto('/tasks');
    const opener = page.getByRole('button', { name: 'New task' });
    await opener.focus();
    await page.keyboard.press('Enter');
    const dialog = page.getByRole('dialog', { name: 'New task' });
    await expect(dialog.getByLabel('Project')).toBeFocused();

    // More presses than the dialog has controls, so Tab wraps at least twice.
    for (let i = 0; i < 25; i++) {
      await page.keyboard.press('Tab');
      expect(await dialog.evaluate((el) => el.contains(document.activeElement)), `Tab press ${i + 1}`).toBe(true);
    }
    for (let i = 0; i < 12; i++) {
      await page.keyboard.press('Shift+Tab');
      expect(await dialog.evaluate((el) => el.contains(document.activeElement)), `Shift+Tab press ${i + 1}`).toBe(true);
    }

    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test("a field's hint is its description, not part of its name", async ({ page }) => {
    await page.goto('/tasks');
    await page.getByRole('button', { name: 'New task' }).click();
    const body = page.getByRole('textbox', { name: 'Body', exact: true });
    await expect(body).toHaveAccessibleDescription('Markdown is supported.');
    await expect(page.getByRole('spinbutton', { name: 'Priority', exact: true })).toHaveAccessibleDescription('Higher runs first.');
  });

  test('each page names its tab', async ({ page }) => {
    await page.goto('/tasks');
    await expect(page).toHaveTitle('Tasks · onett');
    await page.goto('/');
    await expect(page).toHaveTitle('Overview · onett');
    // A run has no title of its own, so its tab carries the short ID.
    await page.goto(`/runs/${RUN_ID}`);
    await expect(page).toHaveTitle(`Run ${RUN_ID.slice(-8)} · onett`);
    // A task's tab carries the task's title once it has loaded.
    await page.goto(`/tasks/${TASK_ID}`);
    await expect(page).toHaveTitle(/^Make the dashboard handle absurdly long task .* · onett$/);
    // Moving between pages without a reload renames the tab too.
    await page.getByRole('link', { name: 'Deploys', exact: true }).click();
    await expect(page).toHaveTitle('Deploys · onett');
  });

  test('the first Tab stop skips to the page content', async ({ page }) => {
    await page.goto('/tasks');
    await page.locator('main.main').waitFor();
    const skip = page.getByRole('link', { name: 'Skip to content' });
    await page.keyboard.press('Tab');
    await expect(skip).toBeFocused();
    await expect(skip).toBeInViewport();
    await page.keyboard.press('Enter');
    await expect(page.locator('main#main')).toBeFocused();
    expect(new URL(page.url()).hash).toBe('');
  });
});

/** True when a tap `dy` pixels above the control's top edge, and below its bottom edge, lands on it. */
async function takesTapBeyondEdges(control: Locator, dy: number): Promise<boolean> {
  await control.scrollIntoViewIfNeeded();
  return control.evaluate((el, dy) => {
    const box = el.getBoundingClientRect();
    const x = box.left + box.width / 2;
    const hit = (y: number) => {
      const at = document.elementFromPoint(x, y);
      return !!at && (at === el || el.contains(at));
    };
    return hit(box.top - dy) && hit(box.bottom + dy);
  }, dy);
}

test.describe('tap targets, phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test('an ID chip and a text link take a tap at least 24px high', async ({ page }) => {
    await page.goto(`/tasks/${TASK_ID}`);
    const chip = page.locator('.id-chip').first();
    await chip.waitFor();
    const chipBox = await chip.boundingBox();
    // The chip keeps its 17px look; the area that takes the tap reaches 3.5px past each edge.
    expect(chipBox!.height).toBeLessThan(24);
    expect(chipBox!.height + 2 * 3.5).toBeGreaterThanOrEqual(24);
    expect(await takesTapBeyondEdges(chip, 3.5)).toBe(true);

    await page.goto('/users');
    const link = page.getByRole('button', { name: 'Set password' }).first();
    await link.waitFor();
    expect(await takesTapBeyondEdges(link, 3.5)).toBe(true);
  });
});
