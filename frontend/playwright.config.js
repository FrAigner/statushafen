// @ts-check
import { defineConfig, devices } from '@playwright/test';

// E2E_PORT allows a second Vite dev server (e.g. from a worktree) without disturbing the one on
// 5173. All specs read the port from the same variable.
const PORT = process.env.E2E_PORT || '5173';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL: `http://localhost:${PORT}`,
    // The specs check German texts; without a fixed locale and time zone the result depends on the
    // machine (Playwright defaults to en-US), since the UI follows the browser language.
    locale: 'de-DE',
    timezoneId: 'Europe/Berlin',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // Start the Vite dev server before the tests
  webServer: {
    command: `npm run dev -- --port ${PORT}`,
    url: `http://localhost:${PORT}`,
    reuseExistingServer: !process.env.CI,
    timeout: 120 * 1000,
  },
});
