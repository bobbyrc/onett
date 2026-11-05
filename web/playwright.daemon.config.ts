// Real-daemon journey (npm run test:daemon). Unlike the mocked mobile check, this starts an actual
// `onett serve` with a throwaway HOME and drives the built UI in Chromium against it. The test
// starts and stops the daemon itself (e2e-daemon/daemon.ts). Set ONETT_E2E_BINARY to use a
// prebuilt binary; otherwise the test runs `go build`.
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e-daemon',
  testMatch: '**/*.spec.ts',
  outputDir: './e2e-results/daemon-traces',
  // One daemon, one journey: the steps share state and run in order.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  timeout: 180_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never', outputFolder: 'e2e-results/daemon-report' }]] : 'list',
  use: { browserName: 'chromium', actionTimeout: 15_000, navigationTimeout: 30_000, trace: 'retain-on-failure' },
});
