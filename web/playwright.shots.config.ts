// Before/after screenshots for chat UI PRs (npm run shots:chat). Like the mobile layout check it
// serves the built UI with `vite preview` and mocks the backend in the browser, but it only runs
// e2e/chat-shots.spec.ts, so `npm run test:mobile` never takes these shots.
import { defineConfig } from '@playwright/test';
import { previewPort } from './playwright.port';

// Pin it with ONETT_SHOTS_PORT; by default the OS picks a free port.
const PORT = await previewPort('ONETT_SHOTS_PORT');

export default defineConfig({
  testDir: './e2e',
  testMatch: 'chat-shots.spec.ts',
  outputDir: './e2e-results/shots-traces',
  fullyParallel: true,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    browserName: 'chromium',
  },
  webServer: {
    // Run `npm run build` first (the shots:chat script does). The port is free
    // at startup, so parallel runs do not collide.
    command: `npx vite preview --host 127.0.0.1 --port ${PORT} --strictPort`,
    url: `http://127.0.0.1:${PORT}`,
    reuseExistingServer: false,
  },
});
