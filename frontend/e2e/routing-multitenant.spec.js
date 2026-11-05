import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Deckt die Subdomain-/Host-Erkennung in PublicRoot.vue ab (Regression:
// PaaS-Hosts wie *.onrender.com dürfen NICHT als Mandanten-Slug interpretiert
// werden). Die Platform-Domain-Allowlist selbst (onrender.com, vercel.app, ...)
// lässt sich lokal nicht per Playwright nachstellen (erfordert echte DNS-Namen);
// das ist stattdessen gegen die echte Produktions-URL manuell verifiziert.
test.describe('Multi-Tenant Routing (PublicRoot)', () => {
  test('root localhost ohne Subdomain zeigt die Landing Page', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/`);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.');
  });

  test('<slug>.localhost erkennt den Slug und rendert die Mandanten-Statusseite', async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/`);

    // StatusPage.vue statt LandingPage.vue: kein Hero, sondern der Seitenname in der Kopfzeile
    // (die H1 ist jetzt der Gesamtstatus).
    await expect(page.getByRole('heading', { name: 'Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.' })).not.toBeVisible();
    await expect(page.locator('.sp-brand-name')).toContainText('TechStart');
  });

  test('unbekannter Slug über Subdomain zeigt Fehlerzustand statt Absturz', async ({ page }) => {
    await page.goto(`http://this-slug-does-not-exist.localhost:${PORT}/`);

    // StatusPage.vue versucht den Slug zu laden, findet aber keine Seite –
    // muss einen Fehlerzustand zeigen, nicht crashen (Kern des
    // beschriebenen Symptoms "Daten konnten nicht geladen werden").
    await expect(page.locator('body')).not.toContainText('Uncaught');
  });

  test('host-unabhängiger /status/:slug Pfad funktioniert unabhängig vom Hostnamen', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/status/acme`);
    await expect(page.locator('.sp-brand-name')).toContainText('Acme');
  });
});
