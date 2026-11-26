import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Subscription Manager', () => {
  test('should allow an authenticated user to edit their subscription', async ({ page }) => {
    // 1. Login as global admin on the tenant subdomain
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // 2. Go back to public dashboard for the tenant
    await page.goto(`http://techstart.localhost:${PORT}/`);

    // 3. The page itself decides which state applies –
    // with a subscription the manage button, without one only the neutral hint
    // (no red error message).
    // The header's user menu carries the same text, hence without .user-menu-item.
    const manageButton = page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' });
    const noSubHint = page.locator('.subscribe-no-sub');
    await expect(manageButton.or(noSubHint)).toBeVisible();
    await expect(page.locator('.edit-abo-error')).toHaveCount(0);

    if (await manageButton.isVisible()) {
      // 4. Expanding loads directly; there is no "Mein Abo laden" button anymore
      await page.locator('.edit-abo-header').click();
      await expect(page.locator('.edit-abo-body')).toContainText('admin@statushafen.local');
      await expect(page.locator('.edit-abo-result')).toBeVisible();
      await expect(page.locator('button.btn-abo-lookup')).toHaveCount(0);
    } else {
      await expect(noSubHint).toContainText('noch kein Abonnement');
    }
  });

  // Regression test: the "⚙️ Mein Abo bearbeiten" button in the subscribe CTA used to call
  // /api/me/subscription WITHOUT the required ?slug parameter – the backend always
  // rejected that with 400, which the component showed indiscriminately as
  // "Kein Abonnement gefunden". According to SeedData() the seed user "admin" has
  // a Google Chat subscription without email on "Acme Corp" (linked only via
  // CreatedByUserID) – this reliably exposes the bug, because a *real* result is
  // expected here instead of just "one of the two messages".
  test('CTA-Button "Abonnement verwalten" öffnet den Editor und lädt das echte Abo', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://acme.localhost:${PORT}/`);
    await page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' }).click();

    await expect(page.locator('.edit-abo-body')).toBeVisible();
    await expect(page.locator('.edit-abo-result')).toContainText('Google Chat');
    await expect(page.locator('.edit-abo-result')).toContainText('SEED-DEMO');
  });

  // full path through all four states. Deliberately uses
  // techstart.localhost instead of acme.localhost - the seed user "admin" has
  // NO pre-created subscription there (see SeedData(); the Google Chat demo
  // subscription only exists on "Acme Corp"). The test ends the subscription it
  // created at the end, so the initial state (state B) is stable for repeated
  // runs without relying on a database reset between E2E runs.
  test('vollständiger Abo-Fluss: kein Abonnement -> abonnieren -> verwalten -> beenden', async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/`);

    // State B: no subscription, no error, no manage button.
    await expect(page.locator('.subscribe-no-sub')).toBeVisible();
    await expect(page.locator('.edit-abo-error')).toHaveCount(0);
    await expect(page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' })).toHaveCount(0);

    // Subscribe: the email is prefilled with the account address,
    // component selection stays optional ("all" as default).
    await page.locator('button', { hasText: 'Jetzt abonnieren' }).click();
    const wizard = page.locator('.wizard-modal');
    await wizard.locator('.channel-card').first().click();
    await expect(wizard.locator('#subscribe-email')).toHaveValue('admin@statushafen.local');
    await wizard.locator('button', { hasText: 'Weiter' }).click();
    await wizard.locator('button', { hasText: 'Abonnieren' }).click();
    await expect(wizard.locator('.wizard-success')).toBeVisible();
    await wizard.locator('button', { hasText: 'Fertig' }).click();

    // State C: the manage button appears, no hint anymore.
    const manageButton = page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' });
    await expect(manageButton).toBeVisible();
    await expect(page.locator('.subscribe-no-sub')).toHaveCount(0);

    // Open manage and end the subscription - confirmation happens on the page,
    // not via window.confirm.
    await manageButton.click();
    await expect(page.locator('.edit-abo-result')).toBeVisible();
    await page.locator('button.btn-end-abo').click();
    await expect(page.locator('.edit-abo-end-confirm')).toBeVisible();
    await page.locator('button.btn-end-confirm').click();

    // Back in state B.
    await expect(page.locator('.edit-abo-none')).toBeVisible();
    await expect(page.locator('.subscribe-no-sub')).toBeVisible();
    // The section stays in place with its header while the editor is open
    // (SubscriptionManager.vue: noSubscription || showEditAbo); it only disappears after collapsing.
    await page.locator('.edit-abo-header').click();
    await expect(manageButton).toHaveCount(0);
  });
});
