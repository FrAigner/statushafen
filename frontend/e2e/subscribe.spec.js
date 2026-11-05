import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Public Subscribe Wizard', () => {
  // Use a unique email for each test run to avoid conflicts if DB persists
  const testEmail = `test.e2e.${Date.now()}@example.com`;

  test('should allow a user to subscribe via Email', async ({ page }) => {
    // Navigate to the public status page using a valid tenant subdomain
    await page.goto(`http://techstart.localhost:${PORT}/`);

    // Ensure we are on the public dashboard by checking the CTA
    await expect(page.locator('.subscribe-cta')).toBeVisible();

    // Click on the Abonnieren button
    const subscribeBtn = page.locator('button', { hasText: 'Jetzt abonnieren' });
    await expect(subscribeBtn).toBeVisible();
    await subscribeBtn.click();

    // Wizard Step 1 should appear
    const wizardModal = page.locator('.wizard-modal');
    await expect(wizardModal).toBeVisible();
    await expect(wizardModal).toContainText('Wie möchtest du benachrichtigt werden?');

    // Select Email
    await page.locator('.channel-card', { hasText: 'E-Mail' }).click();

    // Fill in Email
    await page.fill('input[type="email"]', testEmail);

    // Click Weiter
    await page.locator('button.btn-primary', { hasText: 'Weiter' }).click();

    // Wizard Step 2 should appear
    await expect(wizardModal).toContainText('Welche Dienste möchtest du beobachten?');

    // Select the first available component
    const firstCheckbox = page.locator('.comp-list-item input[type="checkbox"]').first();
    // Only check if it's visible (there are components)
    if (await firstCheckbox.isVisible()) {
      await firstCheckbox.check();
    }

    // Since the database might be empty, we just wait for the submit button
    const submitBtn = page.locator('button.btn-primary', { hasText: 'Abonnieren' });
    await expect(submitBtn).toBeVisible();

    // Submit
    await submitBtn.click();

    // Wizard Step 3 should appear. anonyme E-Mail-Abos müssen
    // erst per Link bestätigt werden (Double-Opt-in).
    await expect(wizardModal).toContainText('Bitte bestätige deine E-Mail-Adresse');
    await expect(wizardModal).toContainText(testEmail);

    // Close wizard
    await page.locator('button.btn-primary', { hasText: 'Fertig' }).click();
    
    // Wizard should be closed
    await expect(wizardModal).toBeHidden();
  });
});
