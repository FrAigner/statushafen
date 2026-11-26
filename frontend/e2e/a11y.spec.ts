import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.ts.
const PORT = process.env.E2E_PORT || '5173';

// Accessibility in a real browser.
//
// The axe check in vitest (src/test-support/axe.js) runs in happy-dom without
// layout and CSS and therefore deliberately skips contrast, target sizes and
// page rules (landmarks, one h1, page title, lang). This spec covers that in
// real Chromium: the most important pages once each in light and dark, with
// all rules for WCAG 2.1 A/AA (incl. color-contrast), plus target-size
// (WCAG 2.2 AA) and the best-practice page rules (region, landmark-*,
// page-has-heading-one).
//
// Prerequisite as for the other specs: backend with SEED_DB=true and APP_ENV=development on :8080;
// Playwright starts the Vite dev server itself (playwright.config.ts).
test.use({ locale: 'de-DE' });

const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice'];

// Exceptions only for markup that sit-onyx renders itself and that we cannot
// influence. Every exception needs a justification. Currently none is
// needed: the only findings in onyx components (CTA button text on
// --onyx-color-onyx-500) came from our own color scale in style.css.
const EXCLUDE: string[] = [];
const DISABLED_RULES: string[] = [];

type AxeViolation = Awaited<ReturnType<AxeBuilder['analyze']>>['violations'][number];

const ADMIN = { email: 'admin@statushafen.local', password: 'admin123' };
const ADMIN_HOST = `http://techstart.localhost:${PORT}`;

function formatViolations(violations: AxeViolation[]) {
  return violations
    .map((v) => {
      const nodes = v.nodes.slice(0, 5).map((n) => `    ${n.target.join(' ')}\n      ${n.failureSummary?.split('\n').join(' | ')}`);
      if (v.nodes.length > 5) nodes.push(`    … und ${v.nodes.length - 5} weitere`);
      return `  ${v.id} (${v.impact}): ${v.help}\n${nodes.join('\n')}`;
    })
    .join('\n');
}

async function expectNoViolations(page: Page) {
  // Wait for transitions (theme switch, fade-ins), otherwise
  // color-contrast measures intermediate colors.
  await page.waitForTimeout(400);
  let builder = new AxeBuilder({ page }).withTags(TAGS);
  for (const selector of EXCLUDE) builder = builder.exclude(selector);
  if (DISABLED_RULES.length) builder = builder.disableRules(DISABLED_RULES);
  const { violations } = await builder.analyze();
  expect(violations, `axe-Funde:\n${formatViolations(violations)}`).toEqual([]);
}

async function apiLogin(request: APIRequestContext): Promise<string> {
  const res = await request.post(`http://localhost:${PORT}/api/login`, { data: ADMIN });
  expect(res.ok()).toBeTruthy();
  return (await res.json()).token;
}

// Own status page in list layout so the seeded pages stay unchanged.
// Fixed slug and "create if it does not exist yet": Playwright starts a new
// worker after every failure and thus runs beforeAll again; this still yields
// only one page, and parallel workers share it (deleting it in afterAll would
// pull it out from under the other worker). The page lives in the run's
// throwaway database.
const LIST_SLUG = 'e2e-a11y-liste';
let token: string;

test.beforeAll(async ({ request }) => {
  token = await apiLogin(request);
  const headers = { Authorization: `Bearer ${token}` };
  const created = await request.post(`http://localhost:${PORT}/api/admin/pages`, {
    headers,
    data: { Name: 'E2E Barrierefreiheit', Slug: LIST_SLUG, Description: 'Listen-Layout für den axe-Lauf', PageLayout: 'liste' },
  });
  if (created.status() === 409) return; // already exists
  // If two workers create the page at the same time, the second occasionally answers 500
  // instead of 409. If the page exists by now, that is equivalent for this run.
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
  // The header's user menu (dark bar, panel with toggle and links).
  { name: 'Admin-Benutzermenü (geöffnet)', url: `${ADMIN_HOST}/admin/overview`, ready: '.kpi-grid', admin: true, open: '.user-menu-button', opened: '.user-menu-panel' },
];

for (const theme of ['light', 'dark'] as const) {
  test.describe(`axe WCAG 2.1 AA (${theme === 'light' ? 'hell' : 'dunkel'})`, () => {
    test.beforeEach(async ({ page }) => {
      // Set both: useTheme reads localStorage, sit-onyx and some
      // public pages follow prefers-color-scheme.
      await page.emulateMedia({ colorScheme: theme });
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme);
    });

    for (const p of PAGES) {
      test(p.name, async ({ page }) => {
        // Sign in via token instead of the form: the login route is limited to
        // 20 requests per minute, and 12 UI sign-ins per run push that limit.
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
