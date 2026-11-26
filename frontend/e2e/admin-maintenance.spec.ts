import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see visual.spec.ts.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Admin Maintenance Windows', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // The tenant switcher in the admin area is independent of the hostname and
    // selects the first status page (Acme) by default - switch to TechStart
    // explicitly so the later public check under techstart.localhost
    // belongs to the same status page.
    await page.goto(`http://techstart.localhost:${PORT}/admin/maintenance`);
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.status() === 200),
      page.locator('.tenant-select').selectOption({ label: 'TechStart GmbH' }),
    ]);
    // Only continue once the switch is complete; otherwise reloading the list
    // resets an already opened form.
    await expect(page.getByRole('navigation', { name: 'Brotkrumen' })).toContainText('TechStart GmbH');
    await page.waitForLoadState('networkidle');
  });

  test('should create a maintenance window with status "Geplant" and delete it', async ({ page }) => {
    const title = `E2E Maintenance CRUD ${Date.now()}`;
    await page.locator('button', { hasText: '+ Neues Wartungsfenster' }).click();

    await page.locator('input[placeholder*="Datenbank-Wartung"]').fill(title);
    await page.locator('textarea').fill('E2E Wartungsbeschreibung.');

    const componentCombo = page.getByRole('combobox', { name: 'Betroffene Komponente(n)' });
    await componentCombo.click();
    const listboxId = await componentCombo.getAttribute('aria-controls');
    await page.locator(`#${listboxId} [role="option"]`).first().click();
    await page.keyboard.press('Escape');

    const now = new Date();
    const start = new Date(now.getTime() + 60 * 60 * 1000); // +1h
    const end = new Date(now.getTime() + 3 * 60 * 60 * 1000); // +3h
    const toLocal = (d: Date) => {
      const pad = (n: number) => String(n).padStart(2, '0');
      return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
    };
    const inputs = page.locator('input[type="datetime-local"]');
    await inputs.nth(0).fill(toLocal(start));
    await inputs.nth(1).fill(toLocal(end));

    await page.locator('button', { hasText: 'Speichern' }).click();

    const row = page.locator('tr', { hasText: title });
    await expect(row).toBeVisible();
    await expect(row).toContainText('Geplant');

    // Clean up
    // Open the row menu, then pick the entry
    await page.waitForLoadState('networkidle'); // Reloading closes an open row menu
    await row.locator('.row-menu__button').click();
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await row.getByRole('menuitem', { name: 'Löschen' }).click();
        // own confirmation dialog instead of native confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);
    await expect(page.locator('tr', { hasText: title })).toHaveCount(0);
  });

  test('scheduled maintenance appears in a dedicated section on the public status page', async ({ page }) => {
    const title = `E2E Maintenance Public ${Date.now()}`;
    await page.locator('button', { hasText: '+ Neues Wartungsfenster' }).click();
    await page.locator('input[placeholder*="Datenbank-Wartung"]').fill(title);
    await page.locator('textarea').fill('Sichtbarkeitstest.');

    const componentCombo = page.getByRole('combobox', { name: 'Betroffene Komponente(n)' });
    await componentCombo.click();
    const listboxId = await componentCombo.getAttribute('aria-controls');
    await page.locator(`#${listboxId} [role="option"]`).first().click();
    await page.keyboard.press('Escape');

    const now = new Date();
    const start = new Date(now.getTime() + 60 * 60 * 1000);
    const end = new Date(now.getTime() + 3 * 60 * 60 * 1000);
    const toLocal = (d: Date) => {
      const pad = (n: number) => String(n).padStart(2, '0');
      return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
    };
    const inputs = page.locator('input[type="datetime-local"]');
    await inputs.nth(0).fill(toLocal(start));
    await inputs.nth(1).fill(toLocal(end));
    await page.locator('button', { hasText: 'Speichern' }).click();
    await expect(page.locator('tr', { hasText: title })).toBeVisible();

    await page.goto(`http://techstart.localhost:${PORT}/`);
    await expect(page.getByRole('heading', { name: 'Geplante Wartungsarbeiten', level: 2 })).toBeVisible();
    const card = page.getByRole('article').filter({ hasText: title });
    await expect(card).toBeVisible();
    await expect(card).toContainText('Geplant');

    // Clean up
    await page.goto(`http://techstart.localhost:${PORT}/admin/maintenance`);
    // Open the row menu, then pick the entry
    await page.waitForLoadState('networkidle'); // Reloading closes an open row menu
    await page.locator('tr', { hasText: title }).locator('.row-menu__button').click();
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.request().method() === 'DELETE'),
      (async () => {
        await page.locator('tr', { hasText: title }).getByRole('menuitem', { name: 'Löschen' }).click();
        // own confirmation dialog instead of native confirm()
        await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click();
      })(),
    ]);
  });
});
