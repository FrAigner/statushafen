import { test, expect, type Page, type Route } from '@playwright/test'

// Regression test for the public pages in real Chromium
// (happy-dom computes no layout). The API is fully mocked, no backend
// and no credentials needed:
//   cd frontend && E2E_PORT=5219 npx playwright test public-schmal --reporter=list
// Checked at 320 to 430 px, light and dark:
//  - no horizontal overflow (landing page, status page in both layouts, login),
//  - the slug of a directory card stays on one line (no break inside the word) and carries the full text as title,
//  - without measurements "Keine Daten" is shown instead of "0,00 % verfügbar",
//  - the login page has no additional sign-in button in the header.
const PORT = process.env.E2E_PORT || '5173';
const BASE = `http://localhost:${PORT}`;

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

const PAGES = [
  { ID: 1, Name: 'Acme Corp', Slug: 'acme', Description: 'Status von Acme' },
  { ID: 2, Name: 'Beispiel GmbH', Slug: 'beispiel', Description: '' },
  { ID: 3, Name: 'TechStart', Slug: 'techstart', Description: 'Dienste von TechStart' },
];

function dashboard(layout: string) {
  return {
    page: { ID: 1, Name: 'Acme Corp', Slug: 'acme', Description: 'Status von Acme', PrimaryColor: '', SecondaryColor: '', HeaderStyle: 'classic', PageLayout: layout },
    components: [{ ID: 10, Name: 'API', Status: 'operational', Tags: [], Children: [] }],
    incidents: [],
    notices: [],
  };
}

// 90 days, only today with an observation: overall value null.
function availabilityNoData() {
  const days = [];
  for (let i = 89; i >= 0; i--) {
    const d = new Date(Date.now() - i * 86400000).toISOString().slice(0, 10);
    days.push(i === 0
      ? { date: d, status: 'degraded_performance', availability: 0, downtime_seconds: 24290 }
      : { date: d, status: 'no_data', availability: null, downtime_seconds: 0 });
  }
  return { generated_at: new Date().toISOString(), uptime_percent: null, days };
}

async function mockApi(page: Page, layout = 'standard') {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/public/pages') return json(route, PAGES);
    if (path === '/api/public/orders/enabled') return json(route, { enabled: true });
    if (path === '/api/public/auth-config') return json(route, { password_login: true, providers: [] });
    if (path === '/api/public/pages/acme/dashboard') return json(route, dashboard(layout));
    if (path === '/api/public/pages/acme/availability') return json(route, availabilityNoData());
    return json(route, { error: 'unauthorized' }, 401);
  });
}

const overflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [320, 375, 400, 430]) {
    test.describe(`Öffentlich bei ${width} px (${scheme})`, () => {
      test.use({ viewport: { width, height: 800 }, locale: 'de-DE', colorScheme: scheme });

      test('Startseite: kein Überlauf, Slug in einer Zeile mit title', async ({ page }) => {
        await mockApi(page);
        await page.goto(`${BASE}/`);
        await expect(page.locator('.page-card').first()).toBeVisible();
        await page.waitForLoadState('networkidle');
        expect(await overflow(page)).toBeLessThanOrEqual(0);

        // The slug must not break mid-word: one line, full text in the title.
        // parseFloat coerces the numeric fallback (font size * 1.2) to a string, so the union is cast for TS only.
        const slugs = await page.locator('.page-slug').evaluateAll(els => els.map(e => ({
          title: e.getAttribute('title'),
          text: e.textContent,
          lines: Math.round(e.getBoundingClientRect().height / parseFloat((getComputedStyle(e).lineHeight === 'normal' ? parseFloat(getComputedStyle(e).fontSize) * 1.2 : getComputedStyle(e).lineHeight) as string)),
        })));
        for (const s of slugs) {
          expect(s.title).toBe(s.text);
          expect(s.lines).toBe(1);
        }
      });

      for (const layout of ['standard', 'liste']) {
        test(`Statusseite (${layout}): kein Überlauf, „Keine Daten“ statt 0,00 %`, async ({ page }) => {
          await mockApi(page, layout);
          await page.goto(`${BASE}/status/acme`);
          await expect(page.locator('.sp-hero')).toBeVisible();
          await page.waitForLoadState('networkidle');
          expect(await overflow(page)).toBeLessThanOrEqual(0);

          const section = page.locator('section[aria-labelledby="sp-availability-heading"]');
          await expect(section).toBeVisible();
          await expect(section.locator('.avail__nodata').first()).toHaveText('Keine Daten');
          // The overall row must not carry a percentage (today's tooltip states its own value).
          await expect(section.locator('.avail__percent')).toHaveCount(0);
        });
      }

      test('Anmeldeseite: kein Überlauf und kein Anmelden-Knopf in der Kopfzeile', async ({ page }) => {
        await mockApi(page);
        await page.goto(`${BASE}/login`);
        await expect(page.locator('input[type=email]')).toBeVisible();
        await page.waitForLoadState('networkidle');
        expect(await overflow(page)).toBeLessThanOrEqual(0);
        // only the submit button in the form is called "Anmelden".
        await expect(page.locator('header .login-button')).toHaveCount(0);
      });
    });
  }
}
