import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Login button on the public status page renamed ("Admin Login" -> "Login")
// and replaced by a user icon with a dropdown menu once someone is logged in.
// The menu always shows "Abonnement verwalten" and "Abmelden", but "Dashboard"
// only if the user has admin/dashboard rights for the status page CURRENTLY
// SHOWN (not globally).
test.describe('Header Login/User-Dropdown (öffentliche Statusseite)', () => {
  test('zeigt "Anmelden" statt "Admin Login", solange niemand eingeloggt ist', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/`);

    const loginButton = page.getByRole('button', { name: 'Anmelden', exact: true });
    await expect(loginButton).toBeVisible();
    await expect(page.getByRole('button', { name: 'Admin Login' })).toHaveCount(0);

    await loginButton.click();
    // The header button returns to the status page after sign-in.
    await expect(page).toHaveURL(/\/login\?returnTo=\/&page=acme$/);
  });

  test('zeigt nach dem Login ein User-Icon statt des Login-Buttons', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://acme.localhost:${PORT}/`);

    await expect(page.getByRole('button', { name: 'Anmelden', exact: true })).toHaveCount(0);
    const userMenuTrigger = page.getByRole('button', { name: 'Benutzermenü öffnen' });
    await expect(userMenuTrigger).toBeVisible();
  });

  test('Dropdown eines Global Admins enthält Abonnement verwalten, Dashboard und Abmelden', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://acme.localhost:${PORT}/`);
    await page.getByRole('button', { name: 'Benutzermenü öffnen' }).click();

    await expect(page.locator('.user-menu-item', { hasText: 'Abonnement verwalten' })).toBeVisible();
    await expect(page.locator('.user-menu-item', { hasText: 'Dashboard' })).toBeVisible();
    await expect(page.locator('.user-menu-item', { hasText: 'Abmelden' })).toBeVisible();

    // Logout from the dropdown uses the existing logout logic from useAuth.
    await page.locator('.user-menu-item', { hasText: 'Abmelden' }).click();
    await expect(page).toHaveURL(`http://acme.localhost:${PORT}/`);
    await expect(page.getByRole('button', { name: 'Anmelden', exact: true })).toBeVisible();
  });

  // According to SeedData() the seed user "viewer" is only "Viewer" globally, but
  // explicitly registered as "Page Admin" on "Acme Corp Status" (incl. the new
  // "admin_dashboard" permission). A suitable fixture to check that
  // "Dashboard" is decided per page and not globally.
  test('Seiten-Admin sieht "Dashboard" nur auf der Statusseite, für die er Rechte hat', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('viewer@statushafen.local');
    await page.locator('input[type=password]').fill('viewer123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/components/);

    // On "Acme Corp" (page1) "viewer" is Page Admin -> Dashboard visible.
    await page.goto(`http://localhost:${PORT}/status/acme`);
    await page.getByRole('button', { name: 'Benutzermenü öffnen' }).click();
    await expect(page.locator('.user-menu-item', { hasText: 'Dashboard' })).toBeVisible();

    // On "TechStart GmbH" (page2) "viewer" has no role -> no Dashboard,
    // even though it is the same logged-in user (same JWT).
    await page.goto(`http://localhost:${PORT}/status/techstart`);
    await page.getByRole('button', { name: 'Benutzermenü öffnen' }).click();
    await expect(page.locator('.user-menu-item', { hasText: 'Abonnement verwalten' })).toBeVisible();
    await expect(page.locator('.user-menu-item', { hasText: 'Dashboard' })).toHaveCount(0);
  });
});
