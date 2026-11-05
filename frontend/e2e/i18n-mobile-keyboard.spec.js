import { test, expect } from '@playwright/test';

// E2E-Lücken Sprachumschalter, Mobile-Menü und Tastaturbedienung.
// Die Tests laufen gegen gemockte API-Antworten (page.route), damit sie ohne
// Backend und Datenbank auskommen und nur das Frontend-Verhalten prüfen.
// Browsersprache fest auf Deutsch, damit die Erkennung (navigator.language)
// unabhängig vom Testrechner den Standard liefert.
test.use({ locale: 'de-DE' });

async function mockApi(page, { admin = false } = {}) {
  await page.route('**/api/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/api/me' && admin) {
      return route.fulfill({
        json: { id: 1, email: 'admin@example.test', is_global_admin: true, permissions: '' },
      });
    }
    if (url.pathname === '/api/me') {
      return route.fulfill({ status: 401, json: { error: 'unauthorized' } });
    }
    // Die Übersicht erwartet ein Objekt mit pages-Liste (sonst Render-Fehler, Befund 2).
    if (url.pathname === '/api/admin/overview') {
      return route.fulfill({
        json: { pages: [], total_pages: 0, total_open_incidents: 0, total_subscribers: 0, worst_status: 'operational' },
      });
    }
    return route.fulfill({ json: [] });
  });
}

// Fokus ausschließlich per Tab-Taste herstellen (Befund 1): focus() umgeht die
// Tab-Reihenfolge und würde z. B. tabindex="-1" nicht bemerken. Mit Obergrenze,
// damit ein nicht erreichbares Element als Fehlschlag endet statt zu hängen.
async function tabTo(page, selector, maxTabs = 30) {
  for (let i = 0; i < maxTabs; i++) {
    await page.keyboard.press('Tab');
    const focused = await page.evaluate((sel) => document.activeElement?.matches(sel) ?? false, selector);
    if (focused) return;
  }
  throw new Error(`${selector} per Tab nicht innerhalb von ${maxTabs} Schritten erreichbar`);
}

test.describe('Sprache umschalten (DE/EN)', () => {
  test('wechselt Landing Page von Deutsch auf Englisch und merkt sich die Wahl', async ({ page }) => {
    await mockApi(page);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.');

    await page.locator('select.language-switcher').selectOption('en');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('When something goes down, your customers hear it from you first.');

    await page.reload();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('When something goes down, your customers hear it from you first.');
    await expect(page.locator('select.language-switcher')).toHaveValue('en');
  });

  test('wechselt zurück auf Deutsch', async ({ page }) => {
    await mockApi(page);
    await page.addInitScript(() => localStorage.setItem('preferred_language', 'en'));
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('When something goes down, your customers hear it from you first.');

    await page.locator('select.language-switcher').selectOption('de');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.');
  });
});

test.describe('Mobile-Menü (Admin)', () => {
  test.use({ viewport: { width: 375, height: 700 } });

  // Gemeinsamer Start: Admin-Übersicht auf kleinem Viewport, Menü per Burger geöffnet.
  async function openMenu(page) {
    await mockApi(page, { admin: true });
    await page.addInitScript(() => localStorage.setItem('jwt_token', 'test-token'));
    const errors = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await page.goto('/admin/overview');

    const burger = page.locator('button.burger-btn');
    const nav = page.locator('nav.admin-nav');
    await expect(burger).toHaveAttribute('aria-expanded', 'false');
    await expect(nav).not.toHaveClass(/is-open/);

    await burger.click();
    await expect(burger).toHaveAttribute('aria-expanded', 'true');
    await expect(nav).toHaveClass(/is-open/);
    return { burger, nav, errors };
  }

  test('Klick auf das Overlay schließt die Navigation', async ({ page }) => {
    const { burger, nav, errors } = await openMenu(page);
    await page.locator('.mobile-overlay').click({ position: { x: 360, y: 400 } });
    await expect(burger).toHaveAttribute('aria-expanded', 'false');
    await expect(nav).not.toHaveClass(/is-open/);
    expect(errors).toEqual([]);
  });

  test('zweiter Klick auf den Burger schließt die Navigation', async ({ page }) => {
    const { burger, nav } = await openMenu(page);
    await burger.click();
    await expect(burger).toHaveAttribute('aria-expanded', 'false');
    await expect(nav).not.toHaveClass(/is-open/);
  });

  test('Klick auf einen Navigationslink wechselt die Seite und schließt die Navigation', async ({ page }) => {
    const { burger, nav } = await openMenu(page);
    await nav.locator('a[href="/admin/components"]').click();
    await expect(page).toHaveURL(/\/admin\/components$/);
    await expect(burger).toHaveAttribute('aria-expanded', 'false');
    await expect(nav).not.toHaveClass(/is-open/);
  });

  test('Escape schließt die Navigation', async ({ page }) => {
    const { burger, nav } = await openMenu(page);
    await page.keyboard.press('Escape');
    await expect(burger).toHaveAttribute('aria-expanded', 'false');
    await expect(nav).not.toHaveClass(/is-open/);
  });

  // Fokus beim Öffnen auf den ersten Eintrag, beim Schließen zurück auf den Burger.
  test('Fokus wandert beim Öffnen ins Menü und beim Schließen zurück auf den Burger', async ({ page }) => {
    const { burger, nav } = await openMenu(page);
    await expect(nav.locator('.area-items a').first()).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(burger).toBeFocused();
  });

  // Kopf: Die Marke „Statushafen Admin“ wurde auf „Ad“ abgeschnitten, die Seite scrollte seitlich.
  test('Kopfzeile zeigt „Statushafen Admin“ vollständig und die Seite scrollt nicht seitlich', async ({ page }) => {
    await mockApi(page, { admin: true });
    await page.addInitScript(() => localStorage.setItem('jwt_token', 'test-token'));
    await page.goto('/admin/overview');
    const brand = page.locator('h1.topbar-brand');
    await expect(brand).toBeVisible();
    const clipped = await brand.evaluate((el) => el.scrollWidth > el.clientWidth);
    expect(clipped).toBe(false);
    const sub = page.locator('.brand-sub');
    await expect(sub).toHaveText('Admin');
    const box = await sub.boundingBox();
    expect(box.x + box.width).toBeLessThanOrEqual(375);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBe(0);
  });
});

test.describe('Tastaturbedienung', () => {
  test('Sprachumschalter ist per Tab erreichbar und per Tastatur bedienbar', async ({ page }) => {
    await mockApi(page);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.');

    const select = page.locator('select.language-switcher');
    await tabTo(page, 'select.language-switcher');
    await expect(select).toBeFocused();
    // ArrowDown öffnet unter macOS nur die (für Playwright unsichtbare) Auswahlliste und
    // wechselt nicht; die Anfangsbuchstaben-Suche („E“ für „Englisch“) wirkt auf allen Systemen.
    await page.keyboard.press('E');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('When something goes down, your customers hear it from you first.');
    expect(await page.evaluate(() => localStorage.getItem('preferred_language'))).toBe('en');
  });

  test('Login-Formular lässt sich per Tab und Enter absenden', async ({ page }) => {
    let loginBody = null;
    await mockApi(page);
    await page.route('**/api/login', (route) => {
      loginBody = route.request().postDataJSON();
      return route.fulfill({ status: 401, json: { error: 'invalid' } });
    });
    await page.goto('/login');

    await tabTo(page, 'input[type=email]');
    await expect(page.locator('input[type=email]')).toBeFocused();
    await page.keyboard.type('user@example.test');
    await page.keyboard.press('Tab');
    await expect(page.locator('input[type=password]')).toBeFocused();
    await page.keyboard.type('geheim');
    await page.keyboard.press('Enter');

    await expect.poll(() => loginBody).toMatchObject({ email: 'user@example.test', password: 'geheim' });
  });
});
