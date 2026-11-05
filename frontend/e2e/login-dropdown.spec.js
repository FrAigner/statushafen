import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// Login-Button auf der öffentlichen Statusseite umbenannt
// ("Admin Login" -> "Login") und durch ein User-Icon mit Dropdown-Menü
// ersetzt, sobald jemand eingeloggt ist. Das Menü zeigt "Abonnement verwalten"
// und "Abmelden" immer, "Dashboard" jedoch nur, wenn der Nutzer für die GERADE
// ANGEZEIGTE Statusseite (nicht global) Admin-/Dashboard-Rechte hat.
test.describe('Header Login/User-Dropdown (öffentliche Statusseite)', () => {
  test('zeigt "Anmelden" statt "Admin Login", solange niemand eingeloggt ist', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/`);

    const loginButton = page.getByRole('button', { name: 'Anmelden', exact: true });
    await expect(loginButton).toBeVisible();
    await expect(page.getByRole('button', { name: 'Admin Login' })).toHaveCount(0);

    await loginButton.click();
    await expect(page).toHaveURL(/\/login/);
  });

  test('zeigt nach dem Login ein User-Icon statt des Login-Buttons', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://acme.localhost:${PORT}/`);

    await expect(page.getByRole('button', { name: 'Anmelden', exact: true })).toHaveCount(0);
    const userMenuTrigger = page.getByRole('button', { name: 'Benutzermenü öffnen' });
    await expect(userMenuTrigger).toBeVisible();
  });

  test('Dropdown eines Global Admins enthält Abonnement verwalten, Dashboard und Abmelden', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://acme.localhost:${PORT}/`);
    await page.getByRole('button', { name: 'Benutzermenü öffnen' }).click();

    await expect(page.locator('.user-menu-item', { hasText: 'Abonnement verwalten' })).toBeVisible();
    await expect(page.locator('.user-menu-item', { hasText: 'Dashboard' })).toBeVisible();
    await expect(page.locator('.user-menu-item', { hasText: 'Abmelden' })).toBeVisible();

    // Logout aus dem Dropdown nutzt die bestehende Logout-Logik aus useAuth.
    await page.locator('.user-menu-item', { hasText: 'Abmelden' }).click();
    await expect(page).toHaveURL(`http://acme.localhost:${PORT}/`);
    await expect(page.getByRole('button', { name: 'Anmelden', exact: true })).toBeVisible();
  });

  // Seed-User "viewer" ist laut SeedData() global nur "Viewer", auf
  // "Acme Corp Status" jedoch explizit als "Page Admin" (inkl. der neuen
  // "admin_dashboard"-Berechtigung) eingetragen. Passender Fixture, um zu
  // prüfen, dass "Dashboard" seitenbezogen und nicht global entschieden wird.
  test('Seiten-Admin sieht "Dashboard" nur auf der Statusseite, für die er Rechte hat', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('viewer@statushafen.local');
    await page.locator('input[type=password]').fill('viewer123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/components/);

    // Auf "Acme Corp" (page1) ist "viewer" Page Admin -> Dashboard sichtbar.
    await page.goto(`http://localhost:${PORT}/status/acme`);
    await page.getByRole('button', { name: 'Benutzermenü öffnen' }).click();
    await expect(page.locator('.user-menu-item', { hasText: 'Dashboard' })).toBeVisible();

    // Auf "TechStart GmbH" (page2) hat "viewer" keine Rolle -> kein Dashboard,
    // obwohl es sich um denselben eingeloggten Nutzer (gleiches JWT) handelt.
    await page.goto(`http://localhost:${PORT}/status/techstart`);
    await page.getByRole('button', { name: 'Benutzermenü öffnen' }).click();
    await expect(page.locator('.user-menu-item', { hasText: 'Abonnement verwalten' })).toBeVisible();
    await expect(page.locator('.user-menu-item', { hasText: 'Dashboard' })).toHaveCount(0);
  });
});
