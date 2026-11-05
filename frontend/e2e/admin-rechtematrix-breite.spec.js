import { test, expect } from '@playwright/test';

// A: Die Berechtigungs-Matrix im Rollen-Formular lief rechts über die Formularbreite hinaus
// und verlor die letzte Spalte (D, Löschen). happy-dom rechnet kein Layout, deshalb misst dieser Spec in
// echtem Chromium: Matrix, Scrollbereich und alle Zellen liegen innerhalb des Formulars, die Seite scrollt
// nie waagerecht, und bei Platz (Desktop) braucht die Matrix keinen eigenen Scrollbalken. Bei sehr schmalen
// Fenstern (390 px) darf nur der Matrix-Scrollbereich scrollen. Die API ist gemockt (kein Backend nötig):
//   cd frontend && E2E_PORT=5225 npx playwright test admin-rechtematrix-breite --reporter=list
const PORT = process.env.E2E_PORT || '5173';
const BASE = `http://localhost:${PORT}`;

const PAGE = { ID: 1, Name: 'Acme Corp', Slug: 'acme', Description: '', IsPrivate: false, UpdatedAt: '2026-10-01T10:00:00Z' };
const json = (route, body) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

async function mockApi(page) {
  await page.addInitScript(() => {
    localStorage.setItem('jwt_token', 'mock.jwt.token');
    localStorage.setItem('activeTenantId', '1');
  });
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/me') return json(route, { id: 1, email: 'admin@example.test', is_global_admin: true, permissions: '', name: 'Admin' });
    if (path === '/api/pages' || path === '/api/me/pages') return json(route, [PAGE]);
    if (path === '/api/roles') return json(route, []);
    return json(route, path.endsWith('/settings') ? {} : []);
  });
}

// Misst alles in einem Rutsch im Browser.
const measure = () => {
  const box = (el) => { const r = el.getBoundingClientRect(); return { left: r.left, right: r.right }; };
  const fieldset = document.querySelector('fieldset.perm-matrix');
  const scroll = fieldset.querySelector('.perm-matrix-scroll');
  const form = fieldset.closest('form');
  const lastHeads = [...fieldset.querySelectorAll('thead th')].at(-1);
  const deleteCells = [...fieldset.querySelectorAll('tbody tr[data-area] td:last-child input')].map(box);
  const badges = [...fieldset.querySelectorAll('.perm-badge')].map(box);
  return {
    viewport: document.documentElement.clientWidth,
    pageScroll: document.documentElement.scrollWidth,
    form: box(form),
    fieldset: box(fieldset),
    scroll: box(scroll),
    scrollOverflow: scroll.scrollWidth - scroll.clientWidth,
    lastHead: box(lastHeads),
    deleteCells,
    badges,
    rows: fieldset.querySelectorAll('tbody tr[data-area]').length,
  };
};

for (const { width, narrow } of [{ width: 1568, narrow: false }, { width: 1280, narrow: false }, { width: 390, narrow: true }]) {
  test.describe(`Rechtematrix bei ${width} px`, () => {
    test.use({ viewport: { width, height: 900 }, locale: 'de-DE' });

    test('passt in die Formularbreite, die letzte Spalte bleibt sichtbar', async ({ page }) => {
      await mockApi(page);
      await page.goto(`${BASE}/admin/roles`);
      await page.getByRole('button', { name: /Neue Rolle/ }).click();
      await page.locator('fieldset.perm-matrix').waitFor();
      await page.waitForLoadState('networkidle');

      const m = await page.evaluate(measure);
      expect(m.rows).toBeGreaterThan(10);
      // Die Seite scrollt nie waagerecht.
      expect(m.pageScroll).toBeLessThanOrEqual(m.viewport);
      // Fieldset und Scrollbereich liegen im Formular (1 px Rundung).
      expect(m.fieldset.right).toBeLessThanOrEqual(m.form.right + 1);
      expect(m.fieldset.left).toBeGreaterThanOrEqual(m.form.left - 1);
      expect(m.scroll.right).toBeLessThanOrEqual(m.fieldset.right + 1);
      if (!narrow) {
        // Mit genug Platz braucht die Matrix keinen Scrollbalken: nichts ist abgeschnitten.
        expect(m.scrollOverflow).toBeLessThanOrEqual(0);
        expect(m.lastHead.right).toBeLessThanOrEqual(m.scroll.right + 1);
        for (const c of m.deleteCells) expect(c.right).toBeLessThanOrEqual(m.scroll.right + 1);
        for (const b of m.badges) expect(b.right).toBeLessThanOrEqual(m.scroll.right + 1);
      }
    });
  });
}
