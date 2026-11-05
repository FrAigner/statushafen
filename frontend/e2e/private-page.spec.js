import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Private Status Page', () => {
  const uniqueSlug = `private-e2e-${Date.now()}`;

  test('should require login for private status pages', async ({ page }) => {
    page.on('console', msg => console.log('BROWSER CONSOLE:', msg.text()));
    
    // 1. Login as global admin
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // 2. Go to Admin Status Pages and create a private page
    await page.goto(`http://techstart.localhost:${PORT}/admin/pages`);
    await page.locator('button', { hasText: '+ Neue Statusseite' }).click();
    
    await page.locator('input[placeholder="z.B. Acme Corp Status"]').fill(`Private E2E Page`);
    await page.getByLabel(/^Slug/).fill(uniqueSlug);
    
    // Check "Private Statusseite"
    await page.locator('label', { hasText: 'Private Statusseite (Login erforderlich)' }).click();
    // Default Role is "Zugriff verweigert" (null) by default.
    
    // Kein force-Klick: das Ankreuzen von "Privat" blendet ein Rollen-Dropdown
    // ein und verschiebt den Button; ein forcierter Klick übersprang bisher das
    // nötige Scrollen und traf daneben.
    const saveBtn = page.locator('aside.form-panel button[type="submit"]');
    await saveBtn.scrollIntoViewIfNeeded();
    await saveBtn.click();
    
    // Check if there is an error alert
    const alert = page.locator('.alert-warning');
    if (await alert.isVisible()) {
      throw new Error(`Alert message: ${await alert.innerText()}`);
    }
    
    // Ensure it was saved (appears in the table)
    await expect(page.locator('.data-table')).toContainText(`Private E2E Page`);
    await expect(page.locator('.data-table')).toContainText(`Privat`);

    // 3. Logout
    await page.goto(`http://techstart.localhost:${PORT}/admin/components`);
    // Abmelden steht im Benutzermenü der Kopfzeile.
    await page.getByRole('button', { name: /Benutzermenü/ }).click();
    await page.getByRole('button', { name: 'Abmelden' }).click();
    await expect(page).toHaveURL(`http://techstart.localhost:${PORT}/`);

    // 4. Try to access the private page's dashboard unauthenticated
    await page.goto(`http://${uniqueSlug}.localhost:${PORT}/`);
    
    // Eine private Seite antwortet ohne Berechtigung wie eine fehlende Seite (404);
    // die Oberfläche zeigt "Statusseite nicht verfügbar" mit Weg zum Login.
    await expect(page.locator('h2')).toContainText('Statusseite nicht verfügbar');
    await expect(page.locator('text=Diese Statusseite gibt es nicht oder ist privat')).toBeVisible();

    // 5. Click "Zum Login"
    await page.locator('text=Zum Login').click();
    await expect(page).toHaveURL(new RegExp(`/login\\?returnTo=.*`));

    // 6. Login as Global Admin again
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();

    // The login redirect will take us back to the private page dashboard!
    // Since we are Global Admin, we bypass the UserStatusPageRole check.
    await expect(page.locator('.sp-brand-name')).toContainText('Private E2E Page');
  });
});
