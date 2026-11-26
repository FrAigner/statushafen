import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.ts.
const PORT = process.env.E2E_PORT || '5173';

// Since SSO is configured per status page, this page is no static Google/GitHub guide
// but an OIDC form with a read-only callback address from the server (BASE_URL). SAML is
// gone. A real sign-in flow is checked separately (sso-oidc.spec.ts and
// handlers/auth_sso_test.go).
test.describe('Admin SSO Configuration Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login?returnTo=/admin`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/admin/sso`);
  });

  test('shows the OIDC section and knows neither SAML nor Google/GitHub steps', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'SSO Konfiguration', level: 2 })).toBeVisible();
    await expect(page.getByText('OpenID Connect (OIDC)')).toBeVisible();
    await expect(page.locator('main')).not.toContainText('SAML');
    await expect(page.locator('main')).not.toContainText('Google Cloud Console');
    await expect(page.locator('.provider-tab')).toHaveCount(0);
  });

  // Regression: the callback addresses were hard-coded. Now the server provides them and the
  // UI only displays them: read-only, with the page ID, never as a placeholder.
  test('the callback address is read-only and contains the page id', async ({ page }) => {
    const oidc = page.getByLabel('ACS URL', { exact: true });
    await expect(oidc).toHaveAttribute('readonly', '');
    await expect(oidc).toHaveValue(/\/api\/auth\/oidc\/callback\?page=\d+$/);
  });
});
