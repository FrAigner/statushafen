import { test, expect } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Direction C: row actions live in the row menu (RowMenu), no longer as a row of buttons.
async function rowAction(page, rowText, name) {
  await clickRowMenuItem(page, page.locator('tr', { hasText: rowText }).locator('.row-menu__button'), name, { exact: true });
}
const SAVE = 'aside.form-panel button[type="submit"]';

// Switch the page layout in the admin and check it on the public page.
test.describe('Seiten-Layout (standard / liste)', () => {
  const uniqueSlug = `e2e-layout-${Date.now()}`;
  const pageName = `E2E Layout ${Date.now()}`;

  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
  });

  test('Umschalten im Admin ändert die öffentliche Darstellung', async ({ page }) => {
    // A new page starts with "standard"
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

    // Switch to "liste"
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
    await rowAction(page, pageName, 'Bearbeiten');
    await page.locator('.layout-option', { hasText: 'Liste' }).click();
    await page.locator(SAVE).click();
    await expect(page.locator('.data-table')).toContainText(pageName);

    await page.goto(publicUrl);
    await expect(page.locator('.sp-hero')).toBeVisible();
    await expect(page.locator('.component-tree')).toHaveCount(0);

    // Mobile (400 px): no sideways scrolling
    await page.setViewportSize({ width: 400, height: 800 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    // The header overflowed by 39 px at 400 px (wrapping in PublicLayout.vue, backend-free measurement in public-schmal.spec.js).
    expect(overflow).toBeLessThanOrEqual(0);

    // Dark mode via system setting stays readable and without overflow
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(page.locator('.sp-hero')).toBeVisible();

    // Keyboard: the bar row is focusable
    await page.getByRole('group', { name: /letzten 90 Tagen/ }).first().focus();
    await expect(page.getByRole('group', { name: /letzten 90 Tagen/ }).first()).toBeFocused();

    // Clean up
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, pageName, 'Löschen');
        // own confirmation dialog instead of native confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);
  });
});
