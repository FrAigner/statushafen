import { test, expect } from '@playwright/test';
import { clickRowMenuItem } from './rowMenu.js';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Zeilenaktionen stehen im Zeilenmenü („…“), Eintrag per Rolle wählen.
async function rowAction(page, row, name) {
  await clickRowMenuItem(page, row.getByRole('button', { name: /^Aktionen für/ }), name);
}

test.describe('Admin Subscribers Management', () => {
  const subscriberEmail = `e2e-subscriber-${Date.now()}@example.com`;

  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/admin/subscribers`);
  });

  test('should create an email subscriber, edit it, and delete it', async ({ page }) => {
    // 1. Create (Channel defaultet auf "email")
    await page.locator('button', { hasText: '+ Abonnent hinzufügen' }).click();
    await page.locator('input[placeholder="name@beispiel.de"]').fill(subscriberEmail);
    await page.locator('button', { hasText: 'Speichern' }).click();

    await expect(page.locator('.data-table')).toContainText(subscriberEmail);

    // 2. Edit: Komponenten-IDs einschränken
    await rowAction(page, page.locator('tr', { hasText: subscriberEmail }), 'Bearbeiten');
    // Komponenten wählt man im Mehrfach-Auswahlfeld statt per ID-Liste.
    const combo = page.getByRole('combobox', { name: /Abonnierte Komponenten/ });
    await combo.click();
    await page.locator(`#${await combo.getAttribute('aria-controls')} [role="option"]`).first().click();
    await page.keyboard.press('Escape');
    await page.locator('button', { hasText: 'Speichern' }).click();

    await expect(page.locator('tr', { hasText: subscriberEmail })).not.toContainText('alle');

    // 3. Delete
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await rowAction(page, page.locator('tr', { hasText: subscriberEmail }), 'Löschen');
        // eigener Bestätigungsdialog statt natives confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);

    await expect(page.locator('tr', { hasText: subscriberEmail })).toHaveCount(0);
  });

  test('should switch to Google Chat channel and require a webhook URL', async ({ page }) => {
    await page.locator('button', { hasText: '+ Abonnent hinzufügen' }).click();

    // Stufe 4c: Kanal ist jetzt ein natives Select (FormField).
    await page.getByLabel('Kanal').selectOption('google_chat');

    // E-Mail-Feld verschwindet, Webhook-URL-Feld erscheint stattdessen
    await expect(page.locator('input[placeholder="name@beispiel.de"]')).not.toBeVisible();
    await expect(page.locator('input[type=url]')).toBeVisible();
  });
});
