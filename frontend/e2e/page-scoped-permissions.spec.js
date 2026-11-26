import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Regression test: hasPerm() in the frontend used to check only the user's
// global role (from the login response) and completely ignored page-scoped
// UserStatusPageRole assignments. A user whom an admin had explicitly assigned
// "Page Admin" (or similar) on a specific status page still saw no write
// buttons there. According to SeedData() the seed user "Editor" has exactly
// one UserStatusPageRole: the Editor role ("components:read,incidents:read,
// incidents:create/update/delete"; the old catch-all permission "write" no longer exists) on "Acme Corp Status" — this reproduces the bug without
// having to create a custom role.
test.describe('Page-scoped permissions', () => {
  test('Editor sieht Schreibrechte nur für die Berechtigung, die seine Seiten-Rolle tatsächlich gewährt', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('editor@statushafen.local');
    await page.locator('input[type=password]').fill('editor123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/components/);

    // Editor has "incidents:create" on "Acme Corp Status" -> the button must appear.
    await page.goto(`http://localhost:${PORT}/admin/incidents`);
    await expect(page.locator('button, a', { hasText: '+ Neuer Vorfall' })).toBeVisible();

    // Editor has no "components:create" -> the button must not appear.
    await page.goto(`http://localhost:${PORT}/admin/components`);
    await expect(page.locator('button, a', { hasText: '+ Neue Komponente' })).toHaveCount(0);
  });

  test('Global Admin sieht weiterhin alle Schreib-Buttons', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://localhost:${PORT}/admin/components`);
    await expect(page.locator('button, a', { hasText: '+ Neue Komponente' })).toBeVisible();

    await page.goto(`http://localhost:${PORT}/admin/incidents`);
    await expect(page.locator('button, a', { hasText: '+ Neuer Vorfall' })).toBeVisible();
  });

  // Regression test: the badge always showed currentUser.role, i.e. the
  // global role from the login JWT — regardless of which role the user actually
  // has on the active status page via UserStatusPageRole. According to
  // SeedData() the seed user "viewer" is only "Viewer" globally, but explicitly
  // registered as "Page Admin" on "Acme Corp Status". Users therefore saw
  // "Viewer" in the badge although they actually had full write rights on the
  // page (and admin UIs relying on the badge were misleading).
  // Since the redesign the badge itself sits in the sidebar (bottom left) instead
  // of the top bar.
  test('Badge zeigt die seitenbezogene Rolle, nicht die abweichende globale Rolle', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('viewer@statushafen.local');
    await page.locator('input[type=password]').fill('viewer123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/components/);

    await page.getByRole('button', { name: /Benutzermenü/ }).click();
    await expect(page.locator('.user-menu-info small')).toHaveText('Page Admin');

    // And because the role really is "Page Admin", the components:create
    // button must appear too (Viewer alone would not have it).
    await expect(page.locator('button, a', { hasText: '+ Neue Komponente' })).toBeVisible();
  });
});
