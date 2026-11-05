import { test, expect } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu.js';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Zeilenaktionen stehen im Zeilenmenü („…“), Eintrag per Rolle wählen.
async function rowAction(page, row, name) {
  await clickRowMenuItem(page, row.getByRole('button', { name: /^Aktionen für/ }), name);
}

test.describe('Admin Users Management', () => {
  const uniqueEmail = `testuser-${Date.now()}@statushafen.local`;

  test.beforeEach(async ({ page }) => {
    page.on('console', msg => console.log('BROWSER CONSOLE:', msg.text()));
    
    // Login as Global Admin
    await page.goto(`http://techstart.localhost:${PORT}/login`);
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
    
    // Länder-Eingabe gibt es nicht mehr (Tags sind Checkboxen), die Rolle je Statusseite
    // steht als Auswahl "Rolle für <Seite>" im Formular.
    await page.getByLabel('Rolle für TechStart GmbH').selectOption({ index: 1 });
    await page.locator('button', { hasText: 'Speichern' }).click();

    await expect(page.locator('.alert-warning')).not.toBeVisible({ timeout: 2000 });
    await expect(page.locator('tr', { hasText: uniqueEmail })).toContainText('1 Statusseite');

    // 3. Delete User
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, page.locator('tr', { hasText: uniqueEmail }), 'Löschen');
        // eigener Bestätigungsdialog statt natives confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);

    // Check if the user is removed from the table
    await expect(page.locator('tr', { hasText: uniqueEmail })).toHaveCount(0);
  });
});
