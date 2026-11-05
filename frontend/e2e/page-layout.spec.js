import { test, expect } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu.js';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Richtung C: Zeilenaktionen stehen im Zeilenmenü (RowMenu), nicht mehr als Knopfreihe.
async function rowAction(page, rowText, name) {
  await clickRowMenuItem(page, page.locator('tr', { hasText: rowText }).locator('.row-menu__button'), name, { exact: true });
}
const SAVE = 'aside.form-panel button[type="submit"]';

// Seiten-Layout im Admin umschalten und auf der öffentlichen Seite prüfen.
test.describe('Seiten-Layout (standard / liste)', () => {
  const uniqueSlug = `e2e-layout-${Date.now()}`;
  const pageName = `E2E Layout ${Date.now()}`;

  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
  });

  test('Umschalten im Admin ändert die öffentliche Darstellung', async ({ page }) => {
    // Neue Seite startet mit "standard"
    await page.locator('button', { hasText: '+ Neue Statusseite' }).click();
    await page.fill('input[placeholder*="Acme Corp Status"]', pageName);
    await page.getByLabel(/^Slug/).fill(uniqueSlug);
    await expect(page.locator('input[name="page-layout"][value="standard"]')).toBeChecked();
    await page.locator(SAVE).click();
    await expect(page.locator('.data-table')).toContainText(pageName);

    const publicUrl = `http://${uniqueSlug}.localhost:${PORT}/`;
    await page.goto(publicUrl);
    await expect(page.locator('.component-tree')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Vorfälle', exact: true })).toHaveCount(0);

    // Auf "liste" umstellen
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
    await rowAction(page, pageName, 'Bearbeiten');
    await page.locator('.layout-option', { hasText: 'Liste' }).click();
    await page.locator(SAVE).click();
    await expect(page.locator('.data-table')).toContainText(pageName);

    await page.goto(publicUrl);
    await expect(page.locator('.sp-hero')).toBeVisible();
    await expect(page.locator('.component-tree')).toHaveCount(0);

    // Mobil (400 px): kein seitliches Scrollen
    await page.setViewportSize({ width: 400, height: 800 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    // Die Kopfzeile lief bei 400 px um 39 px über (Umbruch in PublicLayout.vue, Messung ohne Backend in public-schmal.spec.js).
    expect(overflow).toBeLessThanOrEqual(0);

    // Dunkel nach Systemeinstellung bleibt lesbar und ohne Überlauf
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(page.locator('.sp-hero')).toBeVisible();

    // Tastatur: die Balkenreihe ist fokussierbar
    await page.getByRole('group', { name: /letzten 90 Tagen/ }).first().focus();
    await expect(page.getByRole('group', { name: /letzten 90 Tagen/ }).first()).toBeFocused();

    // Aufräumen
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, pageName, 'Löschen');
        // eigener Bestätigungsdialog statt natives confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);
  });
});
