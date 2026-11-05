import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Admin Incident Detail (Timeline & Post-Mortem)', () => {
  let incidentTitle;

  test.beforeEach(async ({ page }) => {
    // Eindeutig pro Testlauf (nicht nur pro Datei), sonst sammeln sich bei
    // wiederholten Suite-Läufen gleichnamige Incidents an und der spätere
    // Link-Klick per Titel wird mehrdeutig (strict mode violation).
    incidentTitle = `E2E Detail Incident ${Date.now()}-${Math.floor(Math.random() * 100000)}`;
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // Incident anlegen, um von der Liste zum Detail zu navigieren
    await page.goto(`http://techstart.localhost:${PORT}/admin/incidents`);
    await page.locator('button', { hasText: '+ Neuer Vorfall' }).click();
    await page.locator('input[placeholder*="API-Ausfall"]').fill(incidentTitle);
    await page.locator('textarea').fill('Ursprüngliche Beschreibung.');
    const componentCombo = page.getByRole('combobox', { name: 'Betroffene Komponente(n)' });
    await componentCombo.click();
    const listboxId = await componentCombo.getAttribute('aria-controls');
    await page.locator(`#${listboxId} [role="option"]`).first().click();
    await page.locator('button', { hasText: 'Speichern' }).click();
    await expect(page.locator('.data-table-wrapper')).toContainText(incidentTitle);

    await page.locator('a', { hasText: incidentTitle }).click();
    await expect(page).toHaveURL(/\/admin\/incidents\/\d+\/\d+/);
  });

  test('should add a timeline update with a new status', async ({ page }) => {
    await expect(page.locator('text=Noch keine Updates.')).toBeVisible();

    await page.getByRole('combobox', { name: 'Neuer Status' }).selectOption({ label: 'Identifiziert' });

    await page.locator('textarea[placeholder="Was ist der aktuelle Stand?"]').fill('Root Cause wurde identifiziert, Fix in Arbeit.');
    await page.locator('button', { hasText: 'Update hinzufügen' }).click();

    // Timeline-Eintrag erscheint, "Noch keine Updates" verschwindet
    await expect(page.locator('text=Noch keine Updates.')).not.toBeVisible();
    await expect(page.locator('.tl-msg')).toContainText('Root Cause wurde identifiziert');
    await expect(page.locator('.timeline .st-badge').first()).toContainText('Identifiziert');
  });

  test('should save a post-mortem as draft', async ({ page }) => {
    const textareas = page.locator('.pm-grid textarea, .form-group textarea');

    await page.locator('textarea[placeholder="Kurze Zusammenfassung des Vorfalls"]').fill('E2E Zusammenfassung des Vorfalls.');
    await page.locator('textarea[placeholder="Was war die Ursache?"]').fill('E2E Root Cause.');
    await page.locator('textarea[placeholder="Wer/was war betroffen?"]').fill('E2E Impact.');
    await page.locator('textarea[placeholder="Wie wurde es behoben?"]').fill('E2E Resolution.');
    await page.locator('textarea[placeholder="Wie wurde es erkannt?"]').fill('E2E Detection.');

    await page.locator('button', { hasText: 'Als Entwurf speichern' }).click();

    await expect(page.locator('.pm-summary')).toContainText('E2E Zusammenfassung des Vorfalls.');
    await expect(page.locator('.pm-state')).toContainText('Entwurf');
  });
});
