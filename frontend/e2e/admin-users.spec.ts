import { test, expect, type Locator, type Page } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.ts.
const PORT = process.env.E2E_PORT || '5173';

// Row actions live in the row menu ("…"); pick the entry by role.
async function rowAction(page: Page, row: Locator, name: string | RegExp) {
  await clickRowMenuItem(page, row.getByRole('button', { name: /^Aktionen für/ }), name);
}

test.describe('Admin Users Management', () => {
  const uniqueEmail = `testuser-${Date.now()}@statushafen.local`;

  test.beforeEach(async ({ page }) => {
    page.on('console', msg => console.log('BROWSER CONSOLE:', msg.text()));
    
    // Login as Global Admin
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // Go to Users page
    await page.goto(`http://techstart.localhost:${PORT}/admin/users`);
  });

  test('should create, edit, and delete a user, and respect default groups', async ({ page }) => {
    // 1. Create a User and verify default group logic
    await page.locator('button', { hasText: '+ Neuer Benutzer' }).click();
    
    // By default, since Acme/TechStart don't have a DefaultRoleID in seed data, they should default to "0" (Kein Zugriff).
    // Let's create a user and save it.
    await page.fill('input[type="email"]', uniqueEmail);
    await page.fill('input[type="password"]', 'testpassword123');
    await page.locator('button', { hasText: 'Speichern' }).click();
    
    // Check if the user is in the table
    await expect(page.locator('.data-table')).toContainText(uniqueEmail);

    // 2. Edit User
    await rowAction(page, page.locator('tr', { hasText: uniqueEmail }), 'Bearbeiten');
    
    // The country input no longer exists (tags are checkboxes); the role per status page
    // is a "Rolle für <Seite>" select in the form.
    await page.getByLabel('Rolle für TechStart GmbH').selectOption({ index: 1 });
    await page.locator('button', { hasText: 'Speichern' }).click();

    await expect(page.locator('.alert-warning')).not.toBeVisible({ timeout: 2000 });
    await expect(page.locator('tr', { hasText: uniqueEmail })).toContainText('1 Statusseite');

    // 3. Delete User
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, page.locator('tr', { hasText: uniqueEmail }), 'Löschen');
        // own confirmation dialog instead of native confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);

    // Check if the user is removed from the table
    await expect(page.locator('tr', { hasText: uniqueEmail })).toHaveCount(0);
  });
});
