import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Die Seite ist seit der SSO-Konfiguration je Statusseite keine statische
// Google/GitHub-Anleitung mehr, sondern ein OIDC-Formular mit schreibgeschützter
// Rückruf-Adresse vom Server (BASE_URL). SAML gibt es nicht mehr. Ein echter Anmeldefluss
// wird separat geprüft (sso-oidc.spec.js und handlers/auth_sso_test.go).
test.describe('Admin SSO Configuration Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/admin/sso`);
  });

  test('zeigt den Bereich OIDC und kennt weder SAML noch Google/GitHub-Schritte', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'SSO Konfiguration', level: 2 })).toBeVisible();
    await expect(page.getByText('OpenID Connect (OIDC)')).toBeVisible();
    await expect(page.locator('main')).not.toContainText('SAML');
    await expect(page.locator('main')).not.toContainText('Google Cloud Console');
    await expect(page.locator('.provider-tab')).toHaveCount(0);
  });

  // Regression Die Rückruf-Adressen waren hartcodiert. Heute liefert sie der Server und die
  // Oberfläche zeigt sie nur an: schreibgeschützt, mit der Seiten-ID, nie als Platzhalter.
  test('die Rückruf-Adresse ist schreibgeschützt und enthält die Seiten-ID', async ({ page }) => {
    const oidc = page.getByLabel('ACS URL', { exact: true });
    await expect(oidc).toHaveAttribute('readonly', '');
    await expect(oidc).toHaveValue(/\/api\/auth\/oidc\/callback\?page=\d+$/);
  });
});
