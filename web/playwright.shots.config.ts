// Before/after screenshots for chat UI PRs (npm run shots:chat). Like the mobile layout check it
// serves the built UI with `vite preview` and mocks the backend in the browser, but it only runs
// e2e/chat-shots.spec.ts, so `npm run test:mobile` never takes these shots.
import { defineConfig } from '@playwright/test';

const PORT = 4174;

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
    // Run `npm run build` first (the shots:chat script does). A separate port from the mobile
    // check, so both can run at once.
    command: `npx vite preview --host 127.0.0.1 --port ${PORT} --strictPort`,
    url: `http://127.0.0.1:${PORT}`,
    reuseExistingServer: false,
  },
});
