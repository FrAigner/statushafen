import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see visual.spec.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Admin Notices (General Notices)', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // Switch to TechStart so the later public check under
    // techstart.localhost belongs to the same status page (see admin-maintenance.spec.js).
    await page.goto(`http://techstart.localhost:${PORT}/admin/notices`);
    await Promise.all([
      page.waitForResponse(resp => resp.url().includes('/api/') && resp.status() === 200),
      page.locator('.tenant-select').selectOption({ label: 'TechStart GmbH' }),
    ]);
    // Only continue once the switch is complete; otherwise reloading the list
    // resets an already opened form.
    await expect(page.getByRole('navigation', { name: 'Brotkrumen' })).toContainText('TechStart GmbH');
    await page.waitForLoadState('networkidle');
  });

  test('should create a notice and delete it', async ({ page }) => {
    const title = `E2E Notice CRUD ${Date.now()}`;
    await page.locator('button', { hasText: '+ Neue Ankündigung' }).click();

    await page.locator('input[placeholder*="Wartungsrichtlinie"]').fill(title);
    await page.locator('textarea').fill('E2E Ankündigungstext.');
    await page.locator('button', { hasText: 'Speichern' }).click();

    const row = page.locator('tr', { hasText: title });
    await expect(row).toBeVisible();

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

  test('collapsed notice starts collapsed on the public page and can be expanded', async ({ page }) => {
    const title = `E2E Notice Collapsed ${Date.now()}`;
    await page.locator('button', { hasText: '+ Neue Ankündigung' }).click();

    await page.locator('input[placeholder*="Wartungsrichtlinie"]').fill(title);
    await page.locator('textarea').fill('Diese Ankündigung sollte zunächst eingeklappt sein.');

    // Enable the "Eingeklappt anzeigen" (show collapsed) checkbox
    await page.locator('label', { hasText: 'Eingeklappt anzeigen' }).click();

    await page.locator('button', { hasText: 'Speichern' }).click();
    await expect(page.locator('tr', { hasText: title })).toBeVisible();

    await page.goto(`http://techstart.localhost:${PORT}/`);
    await expect(page.locator('h2', { hasText: 'Ankündigungen' })).toBeVisible();

    // The notice itself must not be visible yet, only the expandable
    // summary header
    const toggle = page.locator('.notice-collapsed-toggle');
    await expect(toggle).toBeVisible();
    await expect(page.locator('.notice-card', { hasText: title })).not.toBeVisible();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');

    // Expand
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const card = page.locator('.notice-card', { hasText: title });
    await expect(card).toBeVisible();

    // Collapse again
    await toggle.click();
    await expect(card).not.toBeVisible();

    // Clean up
    await page.goto(`http://techstart.localhost:${PORT}/admin/notices`);
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
