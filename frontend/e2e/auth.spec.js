import { test, expect } from '@playwright/test';

test.describe('Authentication & Login', () => {
  test('should load the login page and allow login with seeded admin credentials', async ({ page }) => {
    // Go to login page
    await page.goto('/login');

    // Check page title / header
    await expect(page.locator('.login-title')).toContainText('Statushafen');

    // Login mit dem geseedeten Admin-Testnutzer über das reguläre Formular
    await page.fill('input[type="email"]', 'admin@statushafen.local');
    await page.fill('input[type="password"]', 'admin123');
    await page.click('button.btn-login');

    // Should navigate to /admin/overview (Standard-Landingpage nach Login)
    await expect(page).toHaveURL(/\/admin\/overview/);

    // And should see the admin panel
    await expect(page.locator('h2')).toContainText('Mandanten-Übersicht');
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
