import { test, expect } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu.js';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Zeilenaktionen stehen im Zeilenmenü („…“), Eintrag per Rolle wählen.
async function rowAction(page, row, name) {
  await clickRowMenuItem(page, row.getByRole('button', { name: /^Aktionen für/ }), name);
}

test.describe('Admin Roles & Permissions', () => {
  const roleName = `E2E Test Rolle ${Date.now()}`;

  test.beforeEach(async ({ page }) => {
    // Login as Global Admin
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/admin/roles`);
  });

  // Die Berechtigungs-Matrix ist eine Tabelle (Bereich x Lesen/Schreiben/Alles), die Kästchen
  // tragen die Berechtigung als value.
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

    // 2. Edit: zusätzliche Berechtigung hinzufügen
    await rowAction(page, page.locator('tr', { hasText: roleName }), 'Bearbeiten');
    await page.locator('input[type=checkbox][value=\"incidents:update\"]').check();
    await page.locator('button', { hasText: 'Speichern' }).click();

    // Die Liste zeigt kompakte Zusammenfassungen; „Ändern“ schließt Lesen ein.
    await expect(page.locator('tr', { hasText: roleName })).toContainText('Vorfälle: Lesen, Ändern');

    // 3. Delete
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, page.locator('tr', { hasText: roleName }), 'Löschen');
        // eigener Bestätigungsdialog statt natives confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);

    await expect(page.locator('tr', { hasText: roleName })).toHaveCount(0);
  });
});
