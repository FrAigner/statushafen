import { test, expect } from '@playwright/test';

test.describe('Authentication & Login', () => {
  test('should load the login page and allow login with seeded admin credentials', async ({ page }) => {
    // Go to login page, explicitly heading for the admin area
    await page.goto('/login?returnTo=/admin');

    // Check page title / header
    await expect(page.locator('.login-title')).toContainText('Statushafen');

    // Log in with the seeded admin test user via the regular form
    await page.fill('input[type="email"]', 'admin@statushafen.local');
    await page.fill('input[type="password"]', 'admin123');
    await page.click('button.btn-login');

    // returnTo=/admin leads into the admin area, AdminLayout opens /admin/overview
    await expect(page).toHaveURL(/\/admin\/overview/);

    // And should see the admin panel
    await expect(page.locator('h2')).toContainText('Mandanten-Übersicht');
  });

  // Without an explicit target the login returns to the landing page, not the admin area.
  test('returns to the landing page after login without returnTo', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[type="email"]', 'admin@statushafen.local');
    await page.fill('input[type="password"]', 'admin123');
    await page.click('button.btn-login');

    await expect(page).toHaveURL(/\/$/);
  });

  // The quiet footer link is the explicit way into the admin area.
  test('the footer admin link logs in towards the admin area', async ({ page }) => {
    await page.goto('/');
    await page.locator('footer').getByRole('link', { name: 'Verwaltung' }).click();
    await expect(page).toHaveURL(/\/login\?returnTo=\/admin$/);
    await page.fill('input[type="email"]', 'admin@statushafen.local');
    await page.fill('input[type="password"]', 'admin123');
    await page.click('button.btn-login');

    await expect(page).toHaveURL(/\/admin\/overview/);
  });

  // A direct visit of a protected route counts as explicit: back there after login.
  test('returns to a protected route visited directly', async ({ page }) => {
    await page.goto('/admin/incidents');
    await expect(page).toHaveURL(/\/login\?returnTo=\/admin\/incidents$/);
    await page.fill('input[type="email"]', 'admin@statushafen.local');
    await page.fill('input[type="password"]', 'admin123');
    await page.click('button.btn-login');

    await expect(page).toHaveURL(/\/admin\/incidents$/);
  });

  test('should show error for invalid credentials', async ({ page }) => {
    await page.goto('/login');

    // Fill form manually
    await page.fill('input[type="email"]', 'wrong@example.com');
    await page.fill('input[type="password"]', 'badpass');
    
    // Submit
    await page.click('button.btn-login');

    // Expect an error message
    const errorAlert = page.locator('.login-alert.error');
    await expect(errorAlert).toBeVisible();
    await expect(errorAlert).toContainText('E-Mail oder Passwort ist falsch.');
  });
});
