import { test, expect } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu.js';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Richtung C: Zeilenaktionen stehen im Zeilenmenü (RowMenu), nicht mehr als Knopfreihe.
async function rowAction(page, rowText, name) {
  await clickRowMenuItem(page, page.locator('tr', { hasText: rowText }).locator('.row-menu__button'), name, { exact: true });
}
const SAVE = 'aside.form-panel button[type="submit"]';

test.describe('Admin Status Pages Management', () => {
  const uniqueSlug = `e2e-page-${Date.now()}`;
  const pageName = `E2E Status Page ${Date.now()}`;

  test.beforeEach(async ({ page }) => {
    // Login as Global Admin
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // Go to Status Pages admin
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
  });

  test('should create, edit, and delete a status page', async ({ page }) => {
    // 1. Create a Status Page
    await page.locator('button', { hasText: '+ Neue Statusseite' }).click();
    
    await page.fill('input[placeholder*="Acme Corp Status"]', pageName);
    await page.getByLabel(/^Slug/).fill(uniqueSlug); // Slug (Platzhalter ist jetzt ein Hinweistext)
    await page.fill('textarea', 'E2E Description');
    await page.locator(SAVE).click();
    
    // Check if the page is in the table
    await expect(page.locator('.data-table')).toContainText(pageName);

    // 2. Edit Status Page
    await rowAction(page, pageName, 'Bearbeiten');
    
    const editedName = `${pageName} - Edited`;
    await page.fill('input[placeholder*="Acme Corp Status"]', editedName);
    await page.locator(SAVE).click();
    
    // Check if the table reflects the edited name
    await expect(page.locator('.data-table')).toContainText(editedName);

    // 3. Delete Status Page
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, editedName, 'Löschen');
        // eigener Bestätigungsdialog statt natives confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);

    // Check if the page is removed from the table
    await expect(page.locator('tr', { hasText: editedName })).toHaveCount(0);
  });

  // Regression test für eine unlayered CSS-Regel (.form-group input)
  // stretchte native Checkboxen auf width:100%, wodurch die "Private Statusseite"-
  // Checkbox riesig und vom Rest des Formulars losgelöst wirkte. Zusätzlich fehlte
  // flex-direction:row, wodurch Checkbox und Label untereinander statt nebeneinander
  // gerendert wurden.
  test('private-page checkbox stays compact and inline with its label', async ({ page }) => {
    await page.locator('.row-menu__button').first().click();
    await page.getByRole('menuitem', { name: 'Bearbeiten', exact: true }).first().click();

    const checkbox = page.locator('#isPrivate');
    const label = page.locator('label[for="isPrivate"]');

    const checkboxBoxBefore = await checkbox.boundingBox();
    expect(checkboxBoxBefore.width).toBeLessThan(30);

    await checkbox.check();
    await expect(checkbox).toBeChecked();

    const checkboxBox = await checkbox.boundingBox();
    const labelBox = await label.boundingBox();

    // Checkbox darf nicht die volle Formularbreite einnehmen (Regression: war ~946px).
    expect(checkboxBox.width).toBeLessThan(30);
    // Checkbox und Label müssen auf derselben Zeile sitzen (Regression: standen
    // untereinander, weil flex-direction von .form-group auf "column" fiel).
    expect(Math.abs(checkboxBox.y - labelBox.y)).toBeLessThan(10);
    // Label direkt rechts neben der Checkbox, nicht horizontal zentriert im Formular.
    expect(labelBox.x - checkboxBox.x).toBeLessThan(40);

    await checkbox.uncheck();
  });
});
