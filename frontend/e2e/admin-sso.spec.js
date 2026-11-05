import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Die Seite ist seit der SSO-Konfiguration je Statusseite keine statische
// Google/GitHub-Anleitung mehr, sondern ein Formular für OIDC und SAML 2.0 mit schreibgeschützten
// Rückruf-Adressen vom Server (BASE_URL). Ein echter Anmeldefluss wird separat geprüft
// (sso-oidc.spec.js, sso-saml.spec.js und handlers/auth_sso_test.go).
test.describe('Admin SSO Configuration Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/admin/sso`);
  });

  test('zeigt die Bereiche OIDC und SAML und kennt keine Google/GitHub-Schritte mehr', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'SSO Konfiguration', level: 2 })).toBeVisible();
    await expect(page.getByText('OpenID Connect (OIDC)')).toBeVisible();
    await expect(page.getByText('SAML 2.0', { exact: true })).toBeVisible();
    await expect(page.locator('main')).not.toContainText('Google Cloud Console');
    await expect(page.locator('.provider-tab')).toHaveCount(0);
  });

  // Regression Die Rückruf-Adressen waren hartcodiert. Heute liefert sie der Server und die
  // Oberfläche zeigt sie nur an: schreibgeschützt, mit der Seiten-ID, nie als Platzhalter.
  test('Rückruf-Adressen sind schreibgeschützt und enthalten die Seiten-ID', async ({ page }) => {
    const oidc = page.getByLabel('ACS URL', { exact: true });
    await expect(oidc).toHaveAttribute('readonly', '');
    await expect(oidc).toHaveValue(/\/api\/auth\/oidc\/callback\?page=\d+$/);

    const samlAcs = page.getByLabel('SP ACS URL (HTTP-POST)');
    await expect(samlAcs).toHaveAttribute('readonly', '');
    await expect(samlAcs).toHaveValue(/\/api\/auth\/saml\/\d+\/acs$/);
    await expect(page.getByLabel('SP Entity ID')).toHaveValue(/\/api\/auth\/saml\/\d+\/metadata$/);
  });
});
