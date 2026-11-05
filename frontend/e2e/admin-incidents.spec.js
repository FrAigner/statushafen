import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe visual.spec.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Admin Incidents Management', () => {
  const incidentTitle = `E2E Incident ${Date.now()}`;

  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/admin/incidents`);
  });

  test('should create, edit, and delete an incident', async ({ page }) => {
    // 1. Create
    await page.locator('button', { hasText: '+ Neuer Vorfall' }).click();

    await page.locator('input[placeholder*="API-Ausfall"]').fill(incidentTitle);
    await page.locator('textarea').fill('E2E Test-Beschreibung des Vorfalls.');

    // Status-Select bleibt auf dem Default "Investigating", Komponente muss gewählt werden.
    // Über aria-controls mit der konkret geöffneten Listbox verknüpft, da mehrere
    // OnyxSelect-Listboxen (Status, Komponente) gleichzeitig - nur per popover
    // versteckt - im DOM existieren.
    const componentCombo = page.getByRole('combobox', { name: 'Betroffene Komponente(n)' });
    await componentCombo.click();
    const listboxId = await componentCombo.getAttribute('aria-controls');
    await page.locator(`#${listboxId} [role="option"]`).first().click();

    await page.locator('button', { hasText: 'Speichern' }).click();

    await expect(page.locator('.data-table-wrapper')).toContainText(incidentTitle);
    await expect(page.locator('tr', { hasText: incidentTitle })).toContainText('Untersuchung');

    // 2. Edit: Titel ändern
    await page.waitForLoadState('networkidle'); // Nachladen schließt ein offenes Zeilenmenü
    await page.locator('tr', { hasText: incidentTitle }).locator('.row-menu__button').click();
    await page.locator('tr', { hasText: incidentTitle }).getByRole('menuitem', { name: 'Bearbeiten' }).click();
    const editedTitle = `${incidentTitle} - Edited`;
    await page.locator('input[placeholder*="API-Ausfall"]').fill(editedTitle);
    await page.locator('button', { hasText: 'Speichern' }).click();

    await expect(page.locator('.data-table-wrapper')).toContainText(editedTitle);

    // 3. Navigation zum Incident-Detail funktioniert
    await page.locator('a', { hasText: editedTitle }).click();
    await expect(page).toHaveURL(/\/admin\/incidents\/\d+\/\d+/);
    await expect(page.getByRole('heading', { name: editedTitle })).toBeVisible();

    // Zurück zur Liste zum Löschen
    await page.goto(`http://techstart.localhost:${PORT}/admin/incidents`);
    // Zeilenmenü öffnen, dann den Eintrag wählen
    await page.waitForLoadState('networkidle'); // Nachladen schließt ein offenes Zeilenmenü
    await page.locator('tr', { hasText: editedTitle }).locator('.row-menu__button').click();
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await page.locator('tr', { hasText: editedTitle }).getByRole('menuitem', { name: 'Löschen' }).click();
        // eigener Bestätigungsdialog statt natives confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);

    await expect(page.locator('tr', { hasText: editedTitle })).toHaveCount(0);
  });

  test('should create an incident affecting multiple components and show both in the table', async ({ page }) => {
    const multiTitle = `E2E Multi-Component Incident ${Date.now()}`;

    await page.locator('button', { hasText: '+ Neuer Vorfall' }).click();
    await page.locator('input[placeholder*="API-Ausfall"]').fill(multiTitle);
    await page.locator('textarea').fill('Betrifft mehrere Komponenten gleichzeitig.');

    const componentCombo = page.getByRole('combobox', { name: 'Betroffene Komponente(n)' });
    await componentCombo.click();
    const listboxId = await componentCombo.getAttribute('aria-controls');
    const options = page.locator(`#${listboxId} [role="option"]`);
    await options.nth(0).click();
    await options.nth(1).click();
    await page.keyboard.press('Escape');

    await page.locator('button', { hasText: 'Speichern' }).click();

    const row = page.locator('tr', { hasText: multiTitle });
    await expect(row).toBeVisible();
    // Beide Komponentennamen kommagetrennt in der Tabelle
    const componentCell = await row.locator('td').nth(1).innerText();
    expect(componentCell.split(',').map(s => s.trim()).length).toBeGreaterThanOrEqual(2);

    // Aufräumen
    // Zeilenmenü öffnen, dann den Eintrag wählen
    await page.waitForLoadState('networkidle'); // Nachladen schließt ein offenes Zeilenmenü
    await row.locator('.row-menu__button').click();
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await row.getByRole('menuitem', { name: 'Löschen' }).click();
        // eigener Bestätigungsdialog statt natives confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);
    await expect(page.locator('tr', { hasText: multiTitle })).toHaveCount(0);
  });

  // Regression test für eine unlayered CSS-Regel (.form-group input)
  // stretchte die interne Checkbox jeder Komponenten-Option auf width:100%, wodurch
  // das Label daneben auf width:0 gequetscht wurde und unsichtbar (aber im DOM
  // vorhanden) war - das Dropdown wirkte "leer", obwohl die Daten korrekt geladen wurden.
  test('component dropdown options are actually visible with readable labels', async ({ page }) => {
    await page.locator('button', { hasText: '+ Neuer Vorfall' }).click();

    const componentCombo = page.getByRole('combobox', { name: 'Betroffene Komponente(n)' });
    await componentCombo.click();
    const listboxId = await componentCombo.getAttribute('aria-controls');
    const options = page.locator(`#${listboxId} [role="option"]`);

    await expect(options.first()).toBeVisible();
    const labelText = options.first().locator('.onyx-select-option__content');
    await expect(labelText).toBeVisible();
    const box = await labelText.boundingBox();
    expect(box.width).toBeGreaterThan(10);

    const checkbox = options.first().locator('.onyx-select-option__checkbox');
    const checkboxBox = await checkbox.boundingBox();
    expect(checkboxBox.width).toBeLessThan(30);

    await page.keyboard.press('Escape');
  });
});
