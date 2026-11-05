import { test, expect } from '@playwright/test';
import { ssoMockRequired, ssoMockSkipReason } from '../src/test-support/ssoMockGate.js';

// SAML-Anmeldung je Statusseite gegen den Mock-SAML-IdP
// (backend/cmd/mocksamlidp, im Compose-Profil "sso" als Dienst mock-saml).
// Voraussetzungen, siehe README "Lokale SSO-Tests mit Mock-SAML":
//   docker compose --profile sso up -d mock-saml   (oder: cd backend && go run ./cmd/mocksamlidp)
//   Backend mit SEED_DB=true und APP_ENV=development
// Ist der Mock nicht erreichbar, überspringt sich der Spec – außer mit
// E2E_REQUIRE_SSO_MOCK=1 (Nachtjob .github/workflows/e2e-sso-nachts.yml).
//
// Ablauf wie sso-oidc.spec.js: eigene Statusseite und eigenes Konto, die
// IdP-Metadaten holt der Spec vom Mock und fügt sie als XML ein (das Backend
// lädt Metadaten-URLs nur per https). Verknüpfen im Profil, danach Anmelden
// über den SAML-Knopf der Statusseite, zuletzt der Fehlerfall einer nicht
// verknüpften Identität.

const BACKEND = process.env.E2E_BACKEND_URL || 'http://localhost:8080';
const MOCK_SAML = process.env.MOCK_SAML_URL || 'http://localhost:8091';
const ADMIN = { email: 'admin@statushafen.local', password: 'admin123' };
const MOCK = { name: 'Mock-SAML', readme: 'Lokale SSO-Tests mit Mock-SAML' };

async function idpMetadata() {
  try {
    const res = await fetch(`${MOCK_SAML}/metadata`, { signal: AbortSignal.timeout(3000) });
    return res.ok ? await res.text() : '';
  } catch {
    return '';
  }
}

async function apiLogin(request, email, password) {
  const res = await request.post(`${BACKEND}/api/login`, { data: { email, password } });
  expect(res.ok(), `Login ${email}: ${res.status()}`).toBeTruthy();
  return (await res.json()).token;
}

function bearer(token) {
  return { Authorization: `Bearer ${token}` };
}

function jwtClaims(token) {
  return JSON.parse(Buffer.from(token.split('.')[1], 'base64url').toString('utf8'));
}

// Füllt das Anmeldeformular des Mocks aus: "username" wird zur (persistenten)
// NameID, E-Mail und Name gehen als Attribute email/name in die Assertion.
async function signInAtMock(page, username, attrs) {
  await expect(page).toHaveURL(new RegExp(`^${MOCK_SAML}/sso`));
  await page.locator('input[name="username"]').fill(username);
  await page.locator('input[name="email"]').fill(attrs.email || '');
  await page.locator('input[name="name"]').fill(attrs.name || '');
  await page.locator('input[type="submit"]').click();
}

async function sessionToken(page) {
  await page.waitForURL((url) => url.origin !== MOCK_SAML && !url.pathname.startsWith('/auth/sso-callback'));
  expect(page.url()).not.toContain('error=sso_failed');
  const token = await page.evaluate(() => localStorage.getItem('jwt_token'));
  expect(token, 'jwt_token im localStorage').toBeTruthy();
  return token;
}

test.describe('SSO: SAML-Login gegen Mock-IdP', () => {
  const run = Date.now();
  const user = { email: `saml-e2e-${run}@example.org`, password: `Pw-${run}-geheim` };
  const nameID = `saml-e2e-${run}`;
  const cleanup = { adminToken: '', pageId: 0, userId: 0 };
  let metadataXML = '';

  test.beforeAll(async () => {
    metadataXML = await idpMetadata();
    const reason = ssoMockSkipReason({
      reachable: metadataXML !== '', required: ssoMockRequired(process.env), issuer: `${MOCK_SAML}/metadata`, mock: MOCK,
    });
    test.skip(reason !== null, reason ?? '');
  });

  test.afterEach(async ({ request }) => {
    if (!cleanup.adminToken) return;
    if (cleanup.userId) await request.delete(`${BACKEND}/api/users/${cleanup.userId}`, { headers: bearer(cleanup.adminToken) });
    if (cleanup.pageId) await request.delete(`${BACKEND}/api/admin/pages/${cleanup.pageId}`, { headers: bearer(cleanup.adminToken) });
    cleanup.userId = 0;
    cleanup.pageId = 0;
  });

  async function samlPage(request, name, slug) {
    cleanup.adminToken = await apiLogin(request, ADMIN.email, ADMIN.password);
    const admin = bearer(cleanup.adminToken);
    const pageRes = await request.post(`${BACKEND}/api/admin/pages`, { headers: admin, data: { Name: name, Slug: slug } });
    expect(pageRes.status(), await pageRes.text()).toBe(201);
    cleanup.pageId = (await pageRes.json()).ID;
    const ssoRes = await request.put(`${BACKEND}/api/admin/pages/${cleanup.pageId}/sso`, {
      headers: admin,
      data: { saml_enabled: true, saml_idp_metadata_xml: metadataXML, saml_attribute_email: 'email', saml_attribute_name: 'name' },
    });
    expect(ssoRes.ok(), await ssoRes.text()).toBeTruthy();
    // Die SP-Metadaten der Seite sind jetzt öffentlich abrufbar.
    const spMeta = await request.get(`${BACKEND}/api/auth/saml/${cleanup.pageId}/metadata`);
    expect(spMeta.ok(), await spMeta.text()).toBeTruthy();
    expect(await spMeta.text()).toContain(`/api/auth/saml/${cleanup.pageId}/acs`);
    return admin;
  }

  test('verknüpftes Konto meldet sich über den Mock an und bekommt eine Sitzung', async ({ page, request }) => {
    const pageName = `SAML E2E ${run}`;
    const admin = await samlPage(request, pageName, `saml-e2e-${run}`);
    const userRes = await request.post(`${BACKEND}/api/users`, { headers: admin, data: user });
    expect(userRes.status(), await userRes.text()).toBe(201);
    cleanup.userId = (await userRes.json()).ID;
    const attrs = { email: user.email, name: 'Erika SAML' };

    // 1. Verknüpfen im Profil.
    await page.goto('/login');
    await page.locator('#login-email').fill(user.email);
    await page.locator('#login-password').fill(user.password);
    await page.locator('form.login-form button[type="submit"]').click();
    await page.waitForURL((url) => !url.pathname.startsWith('/login'));
    await page.goto('/admin/profile');
    await page.locator('[data-test="sso-linkable"]', { hasText: pageName }).getByRole('button').click();
    await signInAtMock(page, nameID, attrs);
    const linkedToken = await sessionToken(page);
    expect(jwtClaims(linkedToken)).toMatchObject({ user_id: cleanup.userId, sso: 'saml' });
    await expect(page).toHaveURL(/\/admin\/profile$/);
    await expect(page.locator('[data-test="sso-linked"]', { hasText: pageName })).toContainText(user.email);

    // 2. Abmelden und über den SAML-Knopf der Statusseite anmelden.
    await page.evaluate(() => localStorage.clear());
    await page.goto(`/status/saml-e2e-${run}`);
    await page.locator('.app-header').getByRole('button', { name: 'Anmelden' }).click();
    await expect(page).toHaveURL(new RegExp(`/login\\?page=saml-e2e-${run}$`));
    await page.locator('a.saml-login-btn').click();
    await signInAtMock(page, nameID, attrs);
    const token = await sessionToken(page);
    expect(jwtClaims(token)).toMatchObject({ user_id: cleanup.userId, sso: 'saml', name: 'Erika SAML' });

    const me = await request.get(`${BACKEND}/api/me`, { headers: bearer(token) });
    expect(me.ok(), await me.text()).toBeTruthy();
    expect(await me.json()).toMatchObject({ email: user.email });
  });

  test('nicht verknüpfte Identität landet mit Fehlermeldung auf der Login-Seite', async ({ page, request }) => {
    await samlPage(request, `SAML E2E Fehler ${run}`, `saml-e2e-fehler-${run}`);

    await page.goto(`/login?page=saml-e2e-fehler-${run}`);
    await page.locator('a.saml-login-btn').click();
    await signInAtMock(page, `unbekannt-${run}`, {});

    await expect(page).toHaveURL(/\/login\?oidc_error=not_linked$/);
    await expect(page.locator('.login-alert.error')).toBeVisible();
  });
});
