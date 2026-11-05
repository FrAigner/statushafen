// @ts-check
import { defineConfig, devices } from '@playwright/test';

// E2E_PORT erlaubt einen zweiten Vite-Dev-Server (z. B. aus einem Worktree), ohne den auf
// 5173 zu stören. Alle Specs lesen den Port aus derselben Variable.
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
    // Die Specs prüfen deutsche Texte; ohne feste Sprache und Zeitzone hängt das Ergebnis
    // vom Rechner ab (Playwright-Standard ist en-US), seit die UI die Browsersprache übernimmt.
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
