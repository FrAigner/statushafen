import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Regressionstest: hasPerm() im Frontend prüfte bisher nur die globale Rolle des
// Nutzers (aus dem Login-Response) und ignorierte seitenbezogene
// UserStatusPageRole-Zuweisungen komplett. Ein Nutzer, dem ein Admin explizit
// "Page Admin" (o.ä.) auf einer bestimmten Statusseite zugewiesen hatte, sah dort
// trotzdem keine Schreib-Buttons. Der seed-User "Editor" hat laut SeedData() genau
// eine UserStatusPageRole: Editor-Rolle ("components:read,incidents:read,
// incidents:create/update/delete", das alte Sammelrecht „write“ gibt es nicht mehr) auf "Acme Corp Status" — das reproduziert den Bug ohne eigens
// eine Custom-Rolle anlegen zu müssen.
test.describe('Page-scoped permissions', () => {
  test('Editor sieht Schreibrechte nur für die Berechtigung, die seine Seiten-Rolle tatsächlich gewährt', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('editor@statushafen.local');
    await page.locator('input[type=password]').fill('editor123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/components/);

    // Editor hat "incidents:create" auf "Acme Corp Status" -> Button muss erscheinen.
    await page.goto(`http://localhost:${PORT}/admin/incidents`);
    await expect(page.locator('button, a', { hasText: '+ Neuer Vorfall' })).toBeVisible();

    // Editor hat kein "components:create" -> Button darf nicht erscheinen.
    await page.goto(`http://localhost:${PORT}/admin/components`);
    await expect(page.locator('button, a', { hasText: '+ Neue Komponente' })).toHaveCount(0);
  });

  test('Global Admin sieht weiterhin alle Schreib-Buttons', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://localhost:${PORT}/admin/components`);
    await expect(page.locator('button, a', { hasText: '+ Neue Komponente' })).toBeVisible();

    await page.goto(`http://localhost:${PORT}/admin/incidents`);
    await expect(page.locator('button, a', { hasText: '+ Neuer Vorfall' })).toBeVisible();
  });

  // Regressionstest: Das Badge zeigte immer currentUser.role, also die
  // globale Rolle aus dem Login-JWT — unabhängig davon, welche Rolle der Nutzer
  // tatsächlich per UserStatusPageRole auf der aktiven Statusseite hat. Der
  // seed-User "viewer" ist laut SeedData() global nur "Viewer", aber auf
  // "Acme Corp Status" explizit als "Page Admin" eingetragen. Nutzer sahen dadurch
  // "Viewer" im Badge, obwohl sie auf der Seite tatsächlich volle Schreibrechte
  // hatten (und Admin-UIs, die sich auf das Badge verließen, waren irreführend).
  // Das Badge selbst sitzt seit der Umstellung in der Sidebar (unten links) statt in
  // der Topbar.
  test('Badge zeigt die seitenbezogene Rolle, nicht die abweichende globale Rolle', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('viewer@statushafen.local');
    await page.locator('input[type=password]').fill('viewer123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/components/);

    await page.getByRole('button', { name: /Benutzermenü/ }).click();
    await expect(page.locator('.user-menu-info small')).toHaveText('Page Admin');

    // Und weil die Rolle tatsächlich "Page Admin" ist, muss auch der
    // components:create-Button erscheinen (Viewer allein hätte den nicht).
    await expect(page.locator('button, a', { hasText: '+ Neue Komponente' })).toBeVisible();
  });
});
