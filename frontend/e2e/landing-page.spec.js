import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Landing Page', () => {
  test('should render hero, feature cards, and list public status pages', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/`);

    // Hero section (confirms that PublicRoot.vue renders the LandingPage, not StatusPage)
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.');
    await expect(page.getByRole('link', { name: 'Beispiel ansehen' })).toBeVisible();

    // Public page list (loaded from /api/public/pages)
    await expect(page.locator('.page-grid')).toBeVisible();
  });

  test('should navigate to a public status page via the directory card', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/`);

    // Wait until the page list has loaded (no more "Lade Statusseiten...")
    await expect(page.locator('text=Lade Statusseiten...')).not.toBeVisible({ timeout: 5000 });

    const firstCard = page.locator('.page-card').first();
    await expect(firstCard).toBeVisible();
    const href = await firstCard.getAttribute('href');
    expect(href).toMatch(/^\/status\//);

    await firstCard.click();
    await expect(page).toHaveURL(/\/status\//);
    // The status page's H1 is the overall status; the page name is in the header.
    await expect(page.locator('.sp-brand-name')).toBeVisible();
    await expect(page.getByRole('heading', { level: 1 })).not.toContainText('Wenn etwas ausfällt');
  });
});
