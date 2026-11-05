import { test, expect } from '@playwright/test';

// Mehrfachauswahl und UND/ODER-Suche auf der öffentlichen Statusseite.
// Läuft gegen gemockte API-Antworten (Daten angelehnt an Acme Corp), damit nur der
// Vite-Dev-Server nötig ist.
const comp = (ID, Name, Description, Status, tag) => ({ ID, Name, Description, Status, Tags: [{ Name: tag }], Children: [] });
const dashboard = {
  page: { ID: 1, Name: 'Acme Corp', PrimaryColor: '', SecondaryColor: '', HeaderStyle: 'classic' },
  components: [
    comp(1, 'API Frankfurt', 'Datenbank-Cluster', 'operational', 'Europa'),
    comp(2, 'Web London', 'Frontend', 'degraded_performance', 'Europa'),
    comp(3, 'Web Dallas', 'Frontend Datenbank', 'operational', 'Nordamerika'),
    comp(4, 'Batch Tokio', 'Jobs', 'operational', 'Asien'),
  ],
  incidents: [],
  notices: [],
};

test.describe('Statusseite: Filter und Suche', () => {
  test.use({ locale: 'de-DE' });

  test.beforeEach(async ({ page }) => {
    await page.route('**/api/**', (route) => {
      const url = route.request().url();
      if (url.includes('/dashboard')) return route.fulfill({ json: dashboard });
      return route.fulfill({ status: 404, json: {} });
    });
    await page.goto('/status/acme-corp');
    await expect(page.locator('.filter-bar')).toBeVisible();
  });

  const chip = (page, name) => page.locator('.tag-group').first().getByRole('button', { name, exact: true });

  test('Mehrfachauswahl Region zeigt beide Regionen und lässt sich zurücksetzen', async ({ page }) => {
    await chip(page, 'Europa').click();
    await chip(page, 'Nordamerika').click();
    await expect(chip(page, 'Europa')).toHaveAttribute('aria-pressed', 'true');
    await expect(chip(page, 'Nordamerika')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.getByRole('status').filter({ hasText: 'Komponente(n)' })).toContainText('3 Komponente(n)');

    await page.locator('.clear-all-btn').click();
    await expect(page.getByRole('status').filter({ hasText: 'Komponente(n)' })).toBeHidden();
    await expect(chip(page, 'Alle')).toHaveAttribute('aria-pressed', 'true');
  });

  test('Suche versteht UND und ODER', async ({ page }) => {
    const input = page.locator('.search-input');
    await input.fill('Frontend UND Datenbank');
    await expect(page.getByRole('status').filter({ hasText: 'Komponente(n)' })).toContainText('1 Komponente(n)');
    await input.fill('Frankfurt ODER Tokio');
    await expect(page.getByRole('status').filter({ hasText: 'Komponente(n)' })).toContainText('2 Komponente(n)');
  });

  test('Hilfe-Tooltip ist per Tastatur bedienbar', async ({ page }) => {
    const help = page.locator('.search-help');
    await expect(help).toBeHidden();
    await page.locator('.search-help-btn').focus();
    await page.keyboard.press('Enter');
    await expect(help).toBeVisible();
    await expect(help).toContainText('Frankfurt UND Datenbank');
    await page.keyboard.press('Escape');
    await expect(help).toBeHidden();
  });
});
