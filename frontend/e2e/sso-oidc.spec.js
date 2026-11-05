import { test, expect } from '@playwright/test';
import { ssoMockRequired, ssoMockSkipReason } from '../src/test-support/ssoMockGate.js';

// OIDC-Anmeldung je Statusseite gegen den
// Mock-OIDC-Dienst aus docker-compose.yml (Profil "sso"). Voraussetzungen,
// siehe README "Lokale SSO-Tests mit Mock-OIDC":
//   docker compose --profile sso up -d mock-oidc
//   Backend mit SEED_DB=true, APP_ENV=development und
//   OIDC_DEV_INSECURE_ISSUERS=http://localhost:8090/statushafen
// Ist der Mock nicht erreichbar, überspringt sich der Spec, damit normale
// E2E-Läufe ohne den Dienst nicht rot werden – außer mit E2E_REQUIRE_SSO_MOCK=1
// (Nachtjob .github/workflows/e2e-sso-nachts.yml), dann schlägt er fehl.
//
// Ablauf: Der Spec legt eine eigene Statusseite und ein eigenes Konto an
// (nicht die Seed-Seiten: Global Admins und Konten mit Rolle auf einer anderen
// Seite dürfen das Seiten-SSO nicht nutzen), verknüpft das Konto mit einer
// Mock-Identität und meldet sich danach allein über den Mock an.
//
// Beides läuft über die Oberfläche: Verknüpfen im Profil
// („Mit SSO verknüpfen“), Anmelden über den Login-Knopf der Statusseite und
// „Mit SSO anmelden“ auf der Login-Seite.

const BACKEND = process.env.E2E_BACKEND_URL || 'http://localhost:8080';
const MOCK_OIDC = process.env.MOCK_OIDC_URL || 'http://localhost:8090';
const ISSUER = `${MOCK_OIDC}/statushafen`;
const ADMIN = { email: 'admin@statushafen.local', password: 'admin123' };

// Prüfung per fetch statt über eine Fixture: So entscheidet beforeAll über das
// Überspringen, bevor überhaupt ein Browser startet.
async function mockReachable() {
  try {
    const res = await fetch(`${ISSUER}/.well-known/openid-configuration`, { signal: AbortSignal.timeout(3000) });
    return res.ok;
  } catch {
    return false;
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

// Füllt das Anmeldeformular des Mocks aus: "username" wird zum sub-Claim,
// das Textfeld nimmt zusätzliche Claims als JSON.
async function signInAtMock(page, subject, claims) {
  await expect(page).toHaveURL(new RegExp(`^${ISSUER}/authorize`));
  await page.locator('input[name="username"]').fill(subject);
  await page.locator('textarea[name="claims"]').fill(JSON.stringify(claims));
  await page.locator('input[type="submit"]').click();
}

// Wartet, bis SSOCallback.vue den Einmal-Code eingelöst hat, und liefert das
// JWT der Sitzung aus dem localStorage.
async function sessionToken(page) {
  await page.waitForURL((url) => !url.pathname.startsWith('/auth/sso-callback'));
  expect(page.url()).not.toContain('error=sso_failed');
  const token = await page.evaluate(() => localStorage.getItem('jwt_token'));
  expect(token, 'jwt_token im localStorage').toBeTruthy();
  return token;
}

test.describe('SSO: OIDC-Login gegen Mock-IdP', () => {
  const run = Date.now();
  const user = { email: `sso-e2e-${run}@example.org`, password: `Pw-${run}-geheim` };
  const subject = `sso-e2e-${run}`;
  const cleanup = { adminToken: '', pageId: 0, userId: 0 };

  test.beforeAll(async () => {
    // Mit E2E_REQUIRE_SSO_MOCK=1 (Nachtjob e2e-sso-nachts.yml) wirft das statt zu überspringen.
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

    // 1. Verknüpfen im Profil: mit Passwort anmelden, „Mit SSO verknüpfen“
    // für die eigene Seite wählen. Der Callback führt zurück ins Profil.
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
    // Der konfigurierte E-Mail-Claim erscheint an der Verknüpfung.
    await expect(page.locator('[data-test="sso-linked"]', { hasText: pageName })).toContainText(user.email);

    // 2. Abmelden und über die Statusseite allein über den Mock anmelden:
    // Login-Knopf im Kopf -> Login-Seite mit ?page= -> „Mit SSO anmelden“.
    await page.evaluate(() => localStorage.clear());
    await page.goto(`/status/sso-e2e-${run}`);
    await page.locator('.app-header').getByRole('button', { name: 'Anmelden' }).click();
    await expect(page).toHaveURL(new RegExp(`/login\\?page=sso-e2e-${run}$`));
    await page.locator('a.oidc-login-btn').click();
    await signInAtMock(page, subject, claims);
    const token = await sessionToken(page);
    expect(jwtClaims(token)).toMatchObject({ user_id: cleanup.userId, sso: 'oidc', name: 'Erika SSO' });

    const me = await request.get(`${BACKEND}/api/me`, { headers: bearer(token) });
    expect(me.ok(), await me.text()).toBeTruthy();
    expect(await me.json()).toMatchObject({ email: user.email });
  });

  // Fehler im Callback erscheinen übersetzt auf der Login-Seite
  // statt als JSON. Eine nicht verknüpfte Mock-Identität ergibt not_linked.
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
