import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Komponentenseite nach Richtung C, ersetzt die veraltete Fassung
// (Region/Geo-Scope-Feld gibt es nicht mehr).
test.describe('Admin Components', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);
    await page.goto(`http://localhost:${PORT}/admin/components`);
  });

  test('Komponente anlegen: Pflichtfeld Name, Erfolgsmeldung, Zeile in der Liste', async ({ page }) => {
    const name = `E2E Komponente ${Date.now()}`;

    await page.getByRole('button', { name: /Neue Komponente/ }).click();
    // Ohne Namen kommt der Fehler am Feld, nicht erst vom Server.
    await page.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.locator('.form-field__message--error')).toContainText('Bitte einen Namen eingeben.');

    await page.getByLabel('Name', { exact: true }).fill(name);
    await page.getByRole('button', { name: 'Speichern' }).click();

    await expect(page.locator('.alert-success')).toContainText('Komponente erstellt.');
    await expect(page.locator('.data-table')).toContainText(name);
  });

  // Gruppe anlegen (ohne Status), Mitglied einordnen, Löschen der
  // nicht leeren Gruppe ist gesperrt.
  test('Gruppe anlegen: ohne Status, Zeile in der Liste, nicht leer nicht löschbar', async ({ page }) => {
    const name = `E2E Gruppe ${Date.now()}`;

    await page.getByRole('button', { name: /Neue Gruppe/ }).click();
    await expect(page.locator('aside.form-panel')).toContainText('Neue Gruppe anlegen');
    await expect(page.locator('aside.form-panel fieldset')).toHaveCount(0);
    await page.getByLabel('Name', { exact: true }).fill(name);
    await page.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.locator('.alert-success')).toContainText('Gruppe erstellt.');
    const row = page.locator('tbody tr', { hasText: name });
    await expect(row).toHaveCount(1);

    // Mitglied einordnen, danach ist Löschen der Gruppe gesperrt.
    await page.getByRole('button', { name: /Neue Komponente/ }).click();
    await page.getByLabel('Name', { exact: true }).fill(`${name} Mitglied`);
    await page.getByLabel('Gruppe', { exact: true }).selectOption({ label: name });
    await page.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.locator('.alert-success')).toContainText('Komponente erstellt.');
    // Die Menüeinträge stehen erst nach dem Öffnen des Zeilenmenüs im Zugänglichkeitsbaum.
    // Die Gruppenzeile steht vor ihrem Mitglied; beide Zeilen enthalten den Namen.
    const groupRow = row.first();
    // Das Nachladen der Liste nach dem Speichern kann das Menü wieder schließen: wiederholen.
    const groupMenu = groupRow.getByRole('button', { name: /Aktionen/ });
    await expect(async () => {
      if ((await groupMenu.getAttribute('aria-expanded')) !== 'true') await groupMenu.click();
      await expect(page.getByRole('menuitem', { name: /Löschen \(Gruppe nicht leer\)/ })).toHaveAttribute('aria-disabled', 'true', { timeout: 2000 });
    }).toPass({ timeout: 15000 });
  });

  test('Formular fluchtet mit der Oberkante der Listenkarte', async ({ page }) => {
    await page.getByRole('button', { name: /Neue Komponente/ }).click();
    await expect(page.locator('aside.form-panel')).toBeVisible();
    // Das Formular fährt mit einer Animation ein; erst die Endlage vergleichen.
    await expect.poll(async () => {
      const card = await page.locator('.comp-card').boundingBox();
      const form = await page.locator('aside.form-panel').boundingBox();
      return Math.abs(form.y - card.y);
    }).toBeLessThanOrEqual(2);
  });

  test('Zeilenmenü: Pfeil unten öffnet, Escape schließt und gibt den Fokus zurück', async ({ page }) => {
    const button = page.locator('.row-menu__button').first();
    await button.focus();
    await page.keyboard.press('ArrowDown');
    await expect(button).toHaveAttribute('aria-expanded', 'true');
    await expect(page.getByRole('menuitem', { name: 'Bearbeiten' }).first()).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(button).toHaveAttribute('aria-expanded', 'false');
    await expect(button).toBeFocused();
  });
});
