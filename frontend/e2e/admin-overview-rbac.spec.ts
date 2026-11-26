import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.ts.
const PORT = process.env.E2E_PORT || '5173';

// Regression test: a non-global admin could open the tenant overview
// (/admin/overview) directly via URL. The real backend did reject the call
// with 403, but AdminOverview.vue then fell back to a degraded mode that
// reloaded ALL status pages unfiltered via /api/pages (this endpoint had no
// permission check at all) — visible as a list of all tenants, including
// ones the user had no access to. Fix: /api/pages now requires global admin,
// the degraded-mode fallback was removed, and a router guard prevents the
// navigation directly in the frontend.
test.describe('Admin Overview RBAC', () => {
  test('Nicht-Global-Admin wird von /admin/overview weggeleitet und sieht keine fremden Mandanten', async ({ page }) => {
    page.on('console', msg => { if (msg.type() === 'error') console.log('CONSOLE ERROR:', msg.text()); });

    // Log in as "Editor" (not a global admin) via the regular login form.
    await page.goto(`http://localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('editor@statushafen.local');
    await page.locator('input[type=password]').fill('editor123');
    await page.locator('button.btn-login').click();

    // The default redirect after login targets /admin/overview, but the router guard
    // must immediately redirect a non-global admin away from it.
    await expect(page).not.toHaveURL(/\/admin\/overview/);
    await expect(page).toHaveURL(/\/admin\/components/);

    // Direct URL navigation must not get through either.
    await page.goto(`http://localhost:${PORT}/admin/overview`);
    await expect(page).not.toHaveURL(/\/admin\/overview/);
    await expect(page).toHaveURL(/\/admin\/components/);

    // The page must not show names of other tenants (e.g. "TechStart") anywhere.
    await expect(page.locator('body')).not.toContainText('TechStart');

    // Nav entries for global-admin-only areas must not be visible.
    await expect(page.locator('.area-items a[href="/admin/overview"]')).toHaveCount(0);
    await expect(page.locator('.area-items a[href="/admin/audit"]')).toHaveCount(0);
    await expect(page.locator('.area-items a[href="/admin/pages"]')).toHaveCount(0);
    await expect(page.locator('.area-items a[href="/admin/users"]')).toHaveCount(0);
  });

  test('Global Admin sieht die Übersicht weiterhin normal', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();

    await expect(page).toHaveURL(/\/admin\/overview/);
    await expect(page.getByRole('heading', { name: 'Mandanten-Übersicht' })).toBeVisible();
    await expect(page.locator('.area-items a[href="/admin/overview"]')).toHaveCount(1);
  });
});
