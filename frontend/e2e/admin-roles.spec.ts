import { test, expect, type Locator, type Page } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.ts.
const PORT = process.env.E2E_PORT || '5173';

// Row actions live in the row menu ("…"); pick the entry by role.
async function rowAction(page: Page, row: Locator, name: string | RegExp) {
  await clickRowMenuItem(page, row.getByRole('button', { name: /^Aktionen für/ }), name);
}

test.describe('Admin Roles & Permissions', () => {
  const roleName = `E2E Test Rolle ${Date.now()}`;

  test.beforeEach(async ({ page }) => {
    // Login as Global Admin
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/admin/roles`);
  });

  // The permission matrix is a table (area x read/write/all); the checkboxes
  // carry the permission as value.
  test('should create a role with a permission matrix, edit, and delete it', async ({ page }) => {
    // 1. Create
    await page.locator('button', { hasText: '+ Neue Rolle' }).click();
    await page.locator('#role-name').fill(roleName);

    await page.locator('input[type=checkbox][value=\"incidents:read\"]').check();
    await page.locator('input[type=checkbox][value=\"components:read\"]').check();

    await page.locator('button', { hasText: 'Speichern' }).click();

    await expect(page.locator('.data-table')).toContainText(roleName);
    await expect(page.locator('tr', { hasText: roleName })).toContainText('Vorfälle: Lesen');
    await expect(page.locator('tr', { hasText: roleName })).toContainText('Komponenten: Lesen');

    // 2. Edit: add an additional permission
    await rowAction(page, page.locator('tr', { hasText: roleName }), 'Bearbeiten');
    await page.locator('input[type=checkbox][value=\"incidents:update\"]').check();
    await page.locator('button', { hasText: 'Speichern' }).click();

    // The list shows compact summaries; "Ändern" (update) includes read.
    await expect(page.locator('tr', { hasText: roleName })).toContainText('Vorfälle: Lesen, Ändern');

    // 3. Delete
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, page.locator('tr', { hasText: roleName }), 'Löschen');
        // own confirmation dialog instead of native confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);

    await expect(page.locator('tr', { hasText: roleName })).toHaveCount(0);
  });
});
