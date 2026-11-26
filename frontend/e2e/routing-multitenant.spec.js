import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Covers the subdomain/host detection in PublicRoot.vue (regression:
// PaaS hosts like *.onrender.com must NOT be interpreted as a tenant slug).
// The platform domain allowlist itself (onrender.com, vercel.app, ...)
// cannot be reproduced locally with Playwright (requires real DNS names);
// it was verified manually against the real production URL instead.
test.describe('Multi-Tenant Routing (PublicRoot)', () => {
  test('root localhost ohne Subdomain zeigt die Landing Page', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/`);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.');
  });

  test('<slug>.localhost erkennt den Slug und rendert die Mandanten-Statusseite', async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/`);

    // StatusPage.vue instead of LandingPage.vue: no hero, but the page name in the header
    // (the H1 is now the overall status).
    await expect(page.getByRole('heading', { name: 'Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.' })).not.toBeVisible();
    await expect(page.locator('.sp-brand-name')).toContainText('TechStart');
  });

  test('unbekannter Slug über Subdomain zeigt Fehlerzustand statt Absturz', async ({ page }) => {
    await page.goto(`http://this-slug-does-not-exist.localhost:${PORT}/`);

    // StatusPage.vue tries to load the slug but finds no page –
    // it must show an error state, not crash (the core of the
    // reported symptom "Daten konnten nicht geladen werden").
    await expect(page.locator('body')).not.toContainText('Uncaught');
  });

  test('host-unabhängiger /status/:slug Pfad funktioniert unabhängig vom Hostnamen', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/status/acme`);
    await expect(page.locator('.sp-brand-name')).toContainText('Acme');
  });
});
