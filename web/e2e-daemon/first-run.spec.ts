// The first-run journey against a real daemon: `onett serve` with a throwaway HOME, then a
// browser doing what a new owner would do, from creating the account to a fully set up factory.
// The test makes no file writes and no API calls of its own; it only reads state back to assert.
//
// Run with `npm run test:daemon` (builds the UI first; see playwright.daemon.config.ts).
import { expect, test, type Page, type Response } from '@playwright/test';
import { mkdirSync, readFileSync, statSync } from 'node:fs';
import { startDaemon, startStub, type Daemon, type Stub } from './daemon';

const SCREENSHOTS = 'e2e-results/daemon-screenshots';
const PASSWORD = 'correct horse battery staple';
// Long and distinctive, so a leak anywhere (page text, a response body) is unmistakable.
const NTFY_TOKEN = 'tk_e2e_9f3c1d77a2b84e65b0c1d2e3f4a5b6c7';
const TOPIC = 'onett-e2e-topic';

test.describe.configure({ mode: 'serial' });

let daemon: Daemon;
let stub: Stub;
let page: Page;
/** Every API response body the browser received, to prove the token never comes back. */
const responseBodies: { url: string; body: string }[] = [];

test.beforeAll(async ({ browser }) => {
  stub = await startStub();
  daemon = await startDaemon();
  page = await browser.newPage();
  page.on('response', (res: Response) => {
    const url = res.url();
    if (!url.includes('/api/') || url.includes('/events')) return; // the event stream never ends
    res
      .text()
      .then((body) => responseBodies.push({ url, body }))
      .catch(() => undefined); // a navigation can cancel a body; nothing to check then
  });
  mkdirSync(SCREENSHOTS, { recursive: true });
});

// A failing step is hard to debug from the browser side alone: attach what the daemon printed.
test.afterEach(async ({}, testInfo) => {
  if (testInfo.status === testInfo.expectedStatus || !daemon) return;
  console.log(`--- daemon log (after "${testInfo.title}" failed) ---\n${daemon.log()}`);
  await testInfo.attach('daemon.log', { body: daemon.log(), contentType: 'text/plain' });
});

test.afterAll(async () => {
  await page?.close();
  await daemon?.stop();
  await stub?.close();
});

/** Saves the visual editor and waits for the "Saved and reloaded." note. No restart notice may appear. */
async function saveVisualEditor() {
  await page.getByRole('region', { name: 'Unsaved changes' }).getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText('Saved and reloaded.')).toBeVisible();
  await expect(page.getByText(/take effect after onett restarts/)).toHaveCount(0);
}

test('create the owner account and see the unfinished checklist', async () => {
  await page.goto(daemon.setupUrl);
  await page.locator('input[name=username]').fill('owner');
  await page.locator('input[name=password]').fill(PASSWORD);
  await page.locator('input[name=confirm]').fill(PASSWORD);
  await page.getByRole('button', { name: 'Create account' }).click();

  const card = page.locator('.setup-checklist');
  await expect(card.getByRole('heading', { name: 'Finish setting up onett' })).toBeVisible();
  const step = (label: string) => card.locator('li', { has: page.getByRole('link', { name: label, exact: true }) });
  // Needing a person: a project, a policy for it, and a way to reach the owner.
  await expect(step('Add a project')).toContainText('To do');
  await expect(step('Choose a policy')).toContainText('To do');
  await expect(step('Reach me')).toContainText('To do');
  // "Agents ready" depends on the host: the default agents need the claude or codex CLI on PATH,
  // which a dev machine has and a clean CI runner does not. So it is not asserted here; the
  // agent step below adds a fake agent that needs no CLI, which makes it done on any host.
  await expect(step('Agents ready')).toBeVisible();
  // No config refers to a secret yet, so none can be missing.
  await expect(step('Secrets set')).toContainText('Done');
  await page.screenshot({ path: `${SCREENSHOTS}/overview-before.png`, fullPage: true });
});

test('add a project from Projects', async () => {
  await page.getByRole('link', { name: 'Projects', exact: true }).click();
  await page.getByRole('button', { name: 'Add project' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add project' });
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill('widgets');
  await dialog.getByRole('textbox', { name: 'Repository', exact: true }).fill('acme/widgets');
  await dialog.getByRole('textbox', { name: 'Local path', exact: true }).fill(daemon.repoDir);
  await dialog.getByRole('button', { name: 'Add project' }).click();
  await expect(dialog).toBeHidden();
  const sheet = page.locator('article.project-sheet', { hasText: 'widgets' });
  await expect(sheet).toContainText('acme/widgets');
  await expect(sheet).toContainText('idle');
});

test('Config → Agents & routing: add a fake harness and an agent that uses it', async () => {
  await page.goto(`${daemon.url}/config?section=agents`);
  await page.getByLabel('New harness name').fill('fake');
  await page.getByRole('button', { name: 'Add harness' }).click();
  await page.getByLabel('New agent name').fill('tester');
  await page.getByRole('button', { name: 'Add agent' }).click();
  // Fields of the new entries.
  // The harness type is an enum, so the editor renders a dropdown. The agent's harness is free text.
  await page.getByRole('combobox', { name: 'Type', exact: true }).selectOption('fake');
  await page.getByRole('textbox', { name: 'Harness', exact: true }).fill('fake');
  await page.getByRole('textbox', { name: 'Prompt text', exact: true }).fill('You are a test agent.');
  await saveVisualEditor();
});

test('Config → project policy: set supervised and see it reload live', async () => {
  await page.goto(`${daemon.url}/config?section=projects`);
  await page.getByRole('textbox', { name: 'Policy' }).fill('supervised');
  await saveVisualEditor();
  await page.screenshot({ path: `${SCREENSHOTS}/policy-saved.png` });
});

test('Config → Setup → ntfy: save with a token and send a test notification', async () => {
  await page.goto(`${daemon.url}/config?mode=setup`);
  await page.getByRole('button', { name: 'ntfy', exact: true }).click();
  const flow = page.getByRole('region', { name: 'ntfy setup' });
  await flow.getByRole('textbox', { name: 'Topic' }).fill(TOPIC);
  await flow.getByRole('textbox', { name: /^Server/ }).fill(stub.url);
  await flow.getByLabel('ntfy access token').fill(NTFY_TOKEN);
  await flow.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(flow.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();

  await flow.getByRole('button', { name: 'Send test notification' }).click();
  await expect(flow.getByRole('status').filter({ hasText: 'Sent. Check that it arrived.' })).toBeVisible();
  expect(stub.requests).toHaveLength(1);
  const [req] = stub.requests;
  expect(req.method).toBe('POST');
  expect(req.path).toBe(`/${TOPIC}`);
  expect(req.headers.authorization).toBe(`Bearer ${NTFY_TOKEN}`);
  expect(req.body.length).toBeGreaterThan(0);
  expect(req.body.toLowerCase()).toContain('test');
});

test('Config → Secrets: the token is set and appears nowhere', async () => {
  await page.goto(`${daemon.url}/config?mode=secrets`);
  const row = page.getByRole('row', { name: /NTFY_TOKEN/ });
  await expect(row.getByRole('cell').nth(1)).toHaveText('set');
  expect(await page.content()).not.toContain(NTFY_TOKEN);
  expect(await page.locator('body').innerText()).not.toContain(NTFY_TOKEN);
  // Every input on the page, including password fields, is empty.
  for (const value of await page.locator('input').evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value))) {
    expect(value).not.toContain(NTFY_TOKEN);
  }
  expect(responseBodies.length).toBeGreaterThan(5);
  const leaks = responseBodies.filter((r) => r.body.includes(NTFY_TOKEN)).map((r) => r.url);
  expect(leaks, 'API responses that contained the token').toEqual([]);
});

test('Overview: the checklist is complete', async () => {
  // The card also hides while its data loads, so wait for all three requests it reads first.
  const loaded = ['/stats', '/projects', '/secrets'].map((end) =>
    page.waitForResponse((res) => new URL(res.url()).pathname.endsWith(end) && res.ok()),
  );
  await page.goto(`${daemon.url}/`);
  await Promise.all(loaded);
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  // The card hides itself when every step is done.
  await expect(page.getByRole('heading', { name: 'Finish setting up onett' })).toHaveCount(0);
  await page.screenshot({ path: `${SCREENSHOTS}/overview-after.png`, fullPage: true });
});

test('on disk: onett.yaml and secrets.env hold the setup', async () => {
  const yaml = readFileSync(daemon.configPath, 'utf8');
  expect(yaml).toMatch(/^projects:\s*$/m);
  expect(yaml).toContain('widgets:');
  expect(yaml).toContain('repo: acme/widgets');
  expect(yaml).toMatch(/policy: supervised/);
  expect(yaml).toContain('type: ntfy');
  expect(yaml).toContain(`topic: ${TOPIC}`);
  expect(yaml).toContain('token_env: NTFY_TOKEN');
  expect(yaml).toContain('type: fake');
  expect(yaml, 'the config names the variable, never the value').not.toContain(NTFY_TOKEN);

  const secrets = readFileSync(daemon.secretsPath, 'utf8');
  expect(secrets.split('\n')).toContain(`NTFY_TOKEN=${NTFY_TOKEN}`);
  expect(statSync(daemon.secretsPath).mode & 0o777).toBe(0o600);
});
