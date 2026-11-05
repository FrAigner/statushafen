import { test, expect } from '@playwright/test';

// Regressionstest für schmale Breiten im Admin. happy-dom
// rechnet kein Layout, deshalb prüft dieser Spec in echtem Chromium bei 375 und
// 390 px, dass
//  - die Seite nicht horizontal scrollt,
//  - Seitenkopf und Hauptbuttons nicht über den rechten Rand ragen,
//  - Badges in den Karten (z. B. das Domain-Badge der Statusseiten) nicht aus
//    der Karte herausragen.
// Die API ist vollständig gemockt (kein Backend, keine Zugangsdaten nötig):
//   cd frontend && E2E_PORT=5207 npx playwright test admin-schmal --reporter=list
// Der Vite-Dev-Server muss auf diesem Port laufen (oder Playwright startet ihn).
const PORT = process.env.E2E_PORT || '5173';
const BASE = `http://localhost:${PORT}`;

const PAGE = {
  ID: 1, Name: 'Acme Corp', Slug: 'acme', Description: 'Status von Acme', IsPrivate: false,
  CustomDomain: 'status.acme-corporation-example.com', CustomDomainStatus: 'pending',
  UpdatedAt: '2026-10-01T10:00:00Z',
}

const json = (route, body, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

async function mockApi(page) {
  await page.addInitScript(() => {
    localStorage.setItem('jwt_token', 'mock.jwt.token')
    localStorage.setItem('activeTenantId', '1')
  })
  await page.route('**/api/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/me') {
      return json(route, { id: 1, email: 'admin@example.test', is_global_admin: true, permissions: '', name: 'Admin' })
    }
    if (path === '/api/pages' || path === '/api/me/pages') return json(route, [PAGE])
    if (route.request().method() !== 'GET') return json(route, {})
    // Alles andere: leere Liste bzw. leeres Objekt
    return json(route, path.endsWith('/settings') ? {} : [])
  })
}

const ROUTES = [
  '/admin/overview', '/admin/pages', '/admin/components', '/admin/users',
  '/admin/roles', '/admin/ui-translations', '/admin/sso', '/admin/tags',
]

for (const width of [375, 390]) {
  test.describe(`Admin bei ${width} px`, () => {
    test.use({ viewport: { width, height: 800 }, locale: 'de-DE' })

    for (const path of ROUTES) {
      test(`${path}: kein horizontaler Überlauf, Kopf und Hauptbuttons im Sichtfeld`, async ({ page }) => {
        await mockApi(page)
        await page.goto(`${BASE}${path}`)
        await page.locator('.page-head__title, h1, h2').first().waitFor()
        await page.waitForLoadState('networkidle')

        const m = await page.evaluate(() => {
          const vw = document.documentElement.clientWidth
          const over = [...document.querySelectorAll('.page-head, .page-head__row, .page-head__actions, .page-head__actions button, .page-head__title, main button, main .onyx-button')]
            .map(e => ({ cls: e.className?.toString().slice(0, 40), r: Math.round(e.getBoundingClientRect().right) }))
            .filter(e => e.r > vw + 1)
          return { vw, sw: document.documentElement.scrollWidth, over }
        })
        expect(m.sw).toBeLessThanOrEqual(m.vw)
        expect(m.over).toEqual([])
      })
    }

    test('/admin/pages: Domain-Badge in der Karte wird nicht abgeschnitten', async ({ page }) => {
      await mockApi(page)
      await page.goto(`${BASE}/admin/pages`)
      const badge = page.locator('.domain-badge').first()
      await badge.waitFor()
      const m = await page.evaluate(() => {
        const b = document.querySelector('.domain-badge')
        const card = b.closest('.card') || b.closest('td')
        const br = b.getBoundingClientRect()
        const cr = card.getBoundingClientRect()
        return { badgeRight: Math.round(br.right), cardRight: Math.round(cr.right), clipped: b.scrollWidth > b.clientWidth + 1 }
      })
      expect(m.badgeRight).toBeLessThanOrEqual(m.cardRight)
      expect(m.clipped).toBe(false)
    })
  })
}
