import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Regressionstest: Ein Nicht-Global-Admin durfte die Mandanten-Übersicht
// (/admin/overview) direkt per URL aufrufen. Das echte Backend lehnte den
// Aufruf zwar mit 403 ab, aber AdminOverview.vue fiel dann auf einen
// Degraded-Mode zurück, der ungefiltert ALLE Statusseiten über /api/pages
// nachlud (dieser Endpunkt hatte gar keine Berechtigungsprüfung) — sichtbar
// als Liste aller Mandanten inkl. solcher, auf die der Nutzer keinen Zugriff
// hatte. Fix: /api/pages erfordert jetzt Global Admin, der Degraded-Mode-
// Fallback wurde entfernt, und ein Router-Guard verhindert die Navigation
// direkt im Frontend.
test.describe('Admin Overview RBAC', () => {
  test('Nicht-Global-Admin wird von /admin/overview weggeleitet und sieht keine fremden Mandanten', async ({ page }) => {
    page.on('console', msg => { if (msg.type() === 'error') console.log('CONSOLE ERROR:', msg.text()); });

    // Login als "Editor" (kein Global Admin) über das reguläre Login-Formular.
    await page.goto(`http://localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('editor@statushafen.local');
    await page.locator('input[type=password]').fill('editor123');
    await page.locator('button.btn-login').click();

    // Der Standard-Redirect nach Login zielt auf /admin/overview, der Router-Guard
    // muss einen Nicht-Global-Admin aber sofort davon wegleiten.
    await expect(page).not.toHaveURL(/\/admin\/overview/);
    await expect(page).toHaveURL(/\/admin\/components/);

    // Auch direkte URL-Navigation darf nicht durchkommen.
    await page.goto(`http://localhost:${PORT}/admin/overview`);
    await expect(page).not.toHaveURL(/\/admin\/overview/);
    await expect(page).toHaveURL(/\/admin\/components/);

    // Die Seite darf an keiner Stelle Namen anderer Mandanten (z.B. "TechStart") zeigen.
    await expect(page.locator('body')).not.toContainText('TechStart');

    // Nav-Einträge für Global-Admin-only Bereiche dürfen nicht sichtbar sein.
    await expect(page.locator('.area-items a[href="/admin/overview"]')).toHaveCount(0);
    await expect(page.locator('.area-items a[href="/admin/audit"]')).toHaveCount(0);
    await expect(page.locator('.area-items a[href="/admin/pages"]')).toHaveCount(0);
    await expect(page.locator('.area-items a[href="/admin/users"]')).toHaveCount(0);
  });

  test('Global Admin sieht die Übersicht weiterhin normal', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();

    await expect(page).toHaveURL(/\/admin\/overview/);
    await expect(page.getByRole('heading', { name: 'Mandanten-Übersicht' })).toBeVisible();
    await expect(page.locator('.area-items a[href="/admin/overview"]')).toHaveCount(1);
  });
});
