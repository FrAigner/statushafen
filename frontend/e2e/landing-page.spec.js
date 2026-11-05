import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Landing Page', () => {
  test('should render hero, feature cards, and list public status pages', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/`);

    // Hero-Bereich (bestätigt, dass PublicRoot.vue die LandingPage rendert, nicht StatusPage)
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.');
    await expect(page.getByRole('link', { name: 'Beispiel ansehen' })).toBeVisible();

    // Öffentliche Seiten-Liste (aus /api/public/pages geladen)
    await expect(page.locator('.page-grid')).toBeVisible();
  });

  test('should navigate to a public status page via the directory card', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/`);

    // Warten bis die Seiten-Liste geladen ist (kein "Lade Statusseiten..." mehr)
    await expect(page.locator('text=Lade Statusseiten...')).not.toBeVisible({ timeout: 5000 });

    const firstCard = page.locator('.page-card').first();
    await expect(firstCard).toBeVisible();
    const href = await firstCard.getAttribute('href');
    expect(href).toMatch(/^\/status\//);

    await firstCard.click();
    await expect(page).toHaveURL(/\/status\//);
    // Die H1 der Statusseite ist der Gesamtstatus; der Seitenname steht in der Kopfzeile.
    await expect(page.locator('.sp-brand-name')).toBeVisible();
    await expect(page.getByRole('heading', { level: 1 })).not.toContainText('Wenn etwas ausfällt');
  });
});
