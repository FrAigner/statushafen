import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Barrierefreiheit im echten Browser.
//
// Die axe-Prüfung in vitest (src/test-support/axe.js) läuft in happy-dom ohne
// Layout und CSS und lässt deshalb Kontrast, Zielgrößen und Seitenregeln
// (Landmarks, eine h1, Seitentitel, lang) bewusst aus. Dieser Spec holt das im
// echten Chromium nach: die wichtigsten Seiten je einmal hell und dunkel, mit
// allen Regeln für WCAG 2.1 A/AA (inkl. color-contrast), dazu target-size
// (WCAG 2.2 AA) und die best-practice-Seitenregeln (region, landmark-*,
// page-has-heading-one).
//
// Voraussetzung wie bei den übrigen Specs: Backend mit SEED_DB=true und APP_ENV=development auf :8080,
// den Vite-Dev-Server startet Playwright selbst (playwright.config.js).
test.use({ locale: 'de-DE' });

const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice'];

// Ausnahmen nur für Markup, das sit-onyx selbst rendert und das wir nicht
// beeinflussen können. Jede Ausnahme braucht eine Begründung. Aktuell
// ist keine nötig: Die einzigen Funde in onyx-Komponenten (CTA-Button-Text auf
// --onyx-color-onyx-500) kamen aus unserer eigenen Farbskala in style.css.
const EXCLUDE = [];
const DISABLED_RULES = [];

const ADMIN = { email: 'admin@statushafen.local', password: 'admin123' };
const ADMIN_HOST = `http://techstart.localhost:${PORT}`;

function formatViolations(violations) {
  return violations
    .map((v) => {
      const nodes = v.nodes.slice(0, 5).map((n) => `    ${n.target.join(' ')}\n      ${n.failureSummary?.split('\n').join(' | ')}`);
      if (v.nodes.length > 5) nodes.push(`    … und ${v.nodes.length - 5} weitere`);
      return `  ${v.id} (${v.impact}): ${v.help}\n${nodes.join('\n')}`;
    })
    .join('\n');
}

async function expectNoViolations(page) {
  // Übergänge (Theme-Wechsel, Einblendungen) abwarten, sonst misst
  // color-contrast Zwischenfarben.
  await page.waitForTimeout(400);
  let builder = new AxeBuilder({ page }).withTags(TAGS);
  for (const selector of EXCLUDE) builder = builder.exclude(selector);
  if (DISABLED_RULES.length) builder = builder.disableRules(DISABLED_RULES);
  const { violations } = await builder.analyze();
  expect(violations, `axe-Funde:\n${formatViolations(violations)}`).toEqual([]);
}

async function apiLogin(request) {
  const res = await request.post(`http://localhost:${PORT}/api/login`, { data: ADMIN });
  expect(res.ok()).toBeTruthy();
  return (await res.json()).token;
}

// Eigene Statusseite im Listen-Layout, damit die geseedeten
// Seiten unverändert bleiben. Fester Slug und "anlegen, falls es sie noch
// nicht gibt": Playwright startet nach jedem Fehlschlag einen neuen Worker und
// damit beforeAll erneut; so entsteht trotzdem nur eine Seite, und parallele
// Worker teilen sie sich (ein Löschen in afterAll würde sie dem anderen Worker
// unter den Füßen wegziehen). Die Seite lebt in der Wegwerf-Datenbank des Laufs.
const LIST_SLUG = 'e2e-a11y-liste';
let token;

test.beforeAll(async ({ request }) => {
  token = await apiLogin(request);
  const headers = { Authorization: `Bearer ${token}` };
  const created = await request.post(`http://localhost:${PORT}/api/admin/pages`, {
    headers,
    data: { Name: 'E2E Barrierefreiheit', Slug: LIST_SLUG, Description: 'Listen-Layout für den axe-Lauf', PageLayout: 'liste' },
  });
  if (created.status() === 409) return; // gibt es schon
  // Legen zwei Worker die Seite gleichzeitig an, antwortet der zweite gelegentlich mit 500
  // statt 409. Existiert die Seite inzwischen, ist das für diesen Lauf gleichwertig.
  if (!created.ok() && (await request.get(`http://localhost:${PORT}/api/public/pages/${LIST_SLUG}/dashboard`)).ok()) return;
  expect(created.ok()).toBeTruthy();
  const pageId = (await created.json()).ID;
  for (const name of ['API', 'Webseite']) {
    const comp = await request.post(`http://localhost:${PORT}/api/pages/${pageId}/components`, {
      headers,
      data: { Name: name, Status: 'operational' },
    });
    expect(comp.ok()).toBeTruthy();
  }
});

const PAGES = [
  { name: 'Landing Page', url: `http://localhost:${PORT}/`, ready: '.page-grid .page-card' },
  { name: 'Statusseite Standard-Layout', url: `http://acme.localhost:${PORT}/`, ready: '.component-tree' },
  { name: 'Statusseite Listen-Layout', url: `http://${LIST_SLUG}.localhost:${PORT}/`, ready: '.sp-hero' },
  { name: 'Login', url: `${ADMIN_HOST}/login`, ready: 'input[type=email]' },
  { name: 'Admin-Übersicht', url: `${ADMIN_HOST}/admin/overview`, ready: '.kpi-grid', admin: true },
  { name: 'Admin-Vorfälle (Liste)', url: `${ADMIN_HOST}/admin/incidents`, ready: '.data-table tbody tr', admin: true },
  { name: 'Profil', url: `${ADMIN_HOST}/admin/profile`, ready: 'h2', admin: true },
  // Das Benutzermenü der Kopfzeile (dunkle Leiste, Panel mit Schalter und Links).
  { name: 'Admin-Benutzermenü (geöffnet)', url: `${ADMIN_HOST}/admin/overview`, ready: '.kpi-grid', admin: true, open: '.user-menu-button', opened: '.user-menu-panel' },
];

for (const theme of ['light', 'dark']) {
  test.describe(`axe WCAG 2.1 AA (${theme === 'light' ? 'hell' : 'dunkel'})`, () => {
    test.beforeEach(async ({ page }) => {
      // Beides setzen: useTheme liest localStorage, sit-onyx und einige
      // öffentliche Seiten folgen prefers-color-scheme.
      await page.emulateMedia({ colorScheme: theme });
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
    });

    for (const p of PAGES) {
      test(p.name, async ({ page }) => {
        // Anmeldung per Token statt über das Formular: Die Login-Route ist auf
        // 20 Anfragen je Minute begrenzt, 12 UI-Anmeldungen je Lauf reizen das aus.
        if (p.admin) await page.addInitScript((tok) => localStorage.setItem('jwt_token', tok), token);
        await page.goto(p.url);
        await expect(page.locator(p.ready).first()).toBeVisible();
        await page.waitForLoadState('networkidle');
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        if (p.open) {
          await page.locator(p.open).click();
          await expect(page.locator(p.opened)).toBeVisible();
        }
        await expectNoViolations(page);
      });
    }
  });
}
