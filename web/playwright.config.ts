// Mobile layout check (npm run test:mobile). It serves the built UI with `vite preview` and
// mocks the whole backend in the browser, so it needs no daemon. Chromium only.
import { defineConfig } from '@playwright/test';

const PORT = 4173;

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.spec.ts',
  // Screenshot capture for PRs (npm run shots:chat) has its own config.
  testIgnore: 'chat-shots.spec.ts',
  outputDir: './e2e-results/traces',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never', outputFolder: 'e2e-results/report' }]] : 'list',
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    browserName: 'chromium',
  },
  webServer: {
    // Run `npm run build` first (the test:mobile script does).
    command: `npx vite preview --host 127.0.0.1 --port ${PORT} --strictPort`,
    url: `http://127.0.0.1:${PORT}`,
    reuseExistingServer: !process.env.CI,
  },
});
