import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { ssoMockRequired, ssoMockSkipReason } from '../src/test-support/ssoMockGate';

// OIDC sign-in per status page against the
// mock OIDC service from docker-compose.yml (profile "sso"). Prerequisites,
// see README "Lokale SSO-Tests mit Mock-OIDC":
//   docker compose --profile sso up -d mock-oidc
//   backend with SEED_DB=true, APP_ENV=development and
//   OIDC_DEV_INSECURE_ISSUERS=http://localhost:8090/statushafen
// If the mock is unreachable, the spec skips itself so normal E2E runs
// without the service do not turn red – except with E2E_REQUIRE_SSO_MOCK=1
// (nightly job .github/workflows/e2e-sso-nachts.yml), where it fails.
//
// Flow: the spec creates its own status page and its own account
// (not the seed pages: global admins and accounts with a role on another
// page may not use the page SSO), links the account to a mock identity
// and then signs in through the mock alone.
//
// Both go through the UI: linking in the profile
// ("Mit SSO verknüpfen"), signing in via the status page's login button and
// "Mit SSO anmelden" on the login page.

const BACKEND = process.env.E2E_BACKEND_URL || 'http://localhost:8080';
const MOCK_OIDC = process.env.MOCK_OIDC_URL || 'http://localhost:8090';
const ISSUER = `${MOCK_OIDC}/statushafen`;
const ADMIN = { email: 'admin@statushafen.local', password: 'admin123' };

// Check via fetch instead of a fixture: this way beforeAll decides on
// skipping before a browser even starts.
async function mockReachable() {
  try {
    const res = await fetch(`${ISSUER}/.well-known/openid-configuration`, { signal: AbortSignal.timeout(3000) });
    return res.ok;
  } catch {
    return false;
  }
}

async function apiLogin(request: APIRequestContext, email: string, password: string): Promise<string> {
  const res = await request.post(`${BACKEND}/api/login`, { data: { email, password } });
  expect(res.ok(), `Login ${email}: ${res.status()}`).toBeTruthy();
  return (await res.json()).token;
}

function bearer(token: string) {
  return { Authorization: `Bearer ${token}` };
}

function jwtClaims(token: string): Record<string, unknown> {
  return JSON.parse(Buffer.from(token.split('.')[1], 'base64url').toString('utf8'));
}

// Fills in the mock's sign-in form: "username" becomes the sub claim,
// the text field takes additional claims as JSON.
async function signInAtMock(page: Page, subject: string, claims: Record<string, unknown>) {
  await expect(page).toHaveURL(new RegExp(`^${ISSUER}/authorize`));
  await page.locator('input[name="username"]').fill(subject);
  await page.locator('textarea[name="claims"]').fill(JSON.stringify(claims));
  await page.locator('input[type="submit"]').click();
}

// Waits until SSOCallback.vue has redeemed the one-time code and returns the
// session JWT from localStorage.
async function sessionToken(page: Page) {
  await page.waitForURL((url) => !url.pathname.startsWith('/auth/sso-callback'));
  expect(page.url()).not.toContain('error=sso_failed');
  const token = await page.evaluate(() => localStorage.getItem('jwt_token'));
  expect(token, 'jwt_token im localStorage').toBeTruthy();
  return token!;
}

test.describe('SSO: OIDC-Login gegen Mock-IdP', () => {
  const run = Date.now();
  const user = { email: `sso-e2e-${run}@example.org`, password: `Pw-${run}-geheim` };
  const subject = `sso-e2e-${run}`;
  const cleanup = { adminToken: '', pageId: 0, userId: 0 };

  test.beforeAll(async () => {
    // With E2E_REQUIRE_SSO_MOCK=1 (nightly job e2e-sso-nachts.yml) this throws instead of skipping.
    const reason = ssoMockSkipReason({ reachable: await mockReachable(), required: ssoMockRequired(process.env), issuer: ISSUER });
    test.skip(reason !== null, reason ?? '');
  });

  test.afterEach(async ({ request }) => {
    if (!cleanup.adminToken) return;
    if (cleanup.userId) await request.delete(`${BACKEND}/api/users/${cleanup.userId}`, { headers: bearer(cleanup.adminToken) });
    if (cleanup.pageId) await request.delete(`${BACKEND}/api/admin/pages/${cleanup.pageId}`, { headers: bearer(cleanup.adminToken) });
  });

  test('verknüpftes Konto meldet sich über den Mock an und bekommt eine Sitzung', async ({ page, request }) => {
    cleanup.adminToken = await apiLogin(request, ADMIN.email, ADMIN.password);
    const admin = bearer(cleanup.adminToken);

    const pageRes = await request.post(`${BACKEND}/api/admin/pages`, {
      headers: admin,
      data: { Name: `SSO E2E ${run}`, Slug: `sso-e2e-${run}` },
    });
    expect(pageRes.status(), await pageRes.text()).toBe(201);
    cleanup.pageId = (await pageRes.json()).ID;

    const ssoRes = await request.put(`${BACKEND}/api/admin/pages/${cleanup.pageId}/sso`, {
      headers: admin,
      data: {
        oidc_enabled: true,
        oidc_discovery_url: `${ISSUER}/.well-known/openid-configuration`,
        oidc_client_id: 'statushafen-local',
        oidc_client_secret: 'beliebig',
        oidc_scopes: 'openid email profile',
        oidc_attribute_email: 'email',
        oidc_attribute_name: 'name',
      },
    });
    expect(ssoRes.ok(), await ssoRes.text()).toBeTruthy();

    const userRes = await request.post(`${BACKEND}/api/users`, { headers: admin, data: user });
    expect(userRes.status(), await userRes.text()).toBe(201);
    cleanup.userId = (await userRes.json()).ID;
    const claims = { email: user.email, name: 'Erika SSO' };

    // 1. Link in the profile: sign in with password, choose "Mit SSO verknüpfen"
    // for our own page. The callback leads back to the profile.
    await page.goto('/login');
    await page.locator('#login-email').fill(user.email);
    await page.locator('#login-password').fill(user.password);
    await page.locator('form.login-form button[type="submit"]').click();
    await page.waitForURL((url) => !url.pathname.startsWith('/login'));
    await page.goto('/admin/profile');
    const pageName = `SSO E2E ${run}`;
    await page.locator('[data-test="sso-linkable"]', { hasText: pageName }).getByRole('button').click();
    await signInAtMock(page, subject, claims);
    const linkedToken = await sessionToken(page);
    expect(jwtClaims(linkedToken)).toMatchObject({ user_id: cleanup.userId, sso: 'oidc' });
    await expect(page).toHaveURL(/\/admin\/profile$/);
    // The configured email claim appears on the link.
    await expect(page.locator('[data-test="sso-linked"]', { hasText: pageName })).toContainText(user.email);

    // 2. Sign out and sign in via the status page through the mock alone:
    // login button in the header -> login page with ?page= -> "Mit SSO anmelden".
    await page.evaluate(() => localStorage.clear());
    await page.goto(`/status/sso-e2e-${run}`);
    await page.locator('.app-header').getByRole('button', { name: 'Anmelden' }).click();
    await expect(page).toHaveURL(new RegExp(`/login\\?returnTo=/status/sso-e2e-${run}&page=sso-e2e-${run}$`));
    await page.locator('a.oidc-login-btn').click();
    await signInAtMock(page, subject, claims);
    const token = await sessionToken(page);
    // Back on the status page the login started from (return_to through the backend).
    await expect(page).toHaveURL(new RegExp(`/status/sso-e2e-${run}$`));
    expect(jwtClaims(token)).toMatchObject({ user_id: cleanup.userId, sso: 'oidc', name: 'Erika SSO' });

    const me = await request.get(`${BACKEND}/api/me`, { headers: bearer(token) });
    expect(me.ok(), await me.text()).toBeTruthy();
    expect(await me.json()).toMatchObject({ email: user.email });
  });

  // Errors in the callback appear translated on the login page
  // instead of as JSON. An unlinked mock identity yields not_linked.
  test('nicht verknüpfte Identität landet mit Fehlermeldung auf der Login-Seite', async ({ page, request }) => {
    cleanup.adminToken = await apiLogin(request, ADMIN.email, ADMIN.password);
    const admin = bearer(cleanup.adminToken);
    const pageRes = await request.post(`${BACKEND}/api/admin/pages`, {
      headers: admin,
      data: { Name: `SSO E2E Fehler ${run}`, Slug: `sso-e2e-fehler-${run}` },
    });
    expect(pageRes.status(), await pageRes.text()).toBe(201);
    cleanup.pageId = (await pageRes.json()).ID;
    const ssoRes = await request.put(`${BACKEND}/api/admin/pages/${cleanup.pageId}/sso`, {
      headers: admin,
      data: {
        oidc_enabled: true,
        oidc_discovery_url: `${ISSUER}/.well-known/openid-configuration`,
        oidc_client_id: 'statushafen-local',
        oidc_client_secret: 'beliebig',
      },
    });
    expect(ssoRes.ok(), await ssoRes.text()).toBeTruthy();

    await page.goto(`/login?page=sso-e2e-fehler-${run}`);
    await page.locator('a.oidc-login-btn').click();
    await signInAtMock(page, `unbekannt-${run}`, {});

    await expect(page).toHaveURL(/\/login\?oidc_error=not_linked$/);
    await expect(page.locator('.login-alert.error')).toBeVisible();
  });
});
