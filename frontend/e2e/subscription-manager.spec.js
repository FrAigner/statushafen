import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Subscription Manager', () => {
  test('should allow an authenticated user to edit their subscription', async ({ page }) => {
    // 1. Login as global admin on the tenant subdomain
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    // 2. Go back to public dashboard for the tenant
    await page.goto(`http://techstart.localhost:${PORT}/`);

    // 3. Die Seite entscheidet selbst, welcher Zustand vorliegt –
    // mit Abonnement der Verwalten-Knopf, ohne Abonnement nur der neutrale Hinweis
    // (keine rote Fehlermeldung).
    // Das Benutzermenü der Kopfzeile trägt denselben Text, daher ohne .user-menu-item.
    const manageButton = page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' });
    const noSubHint = page.locator('.subscribe-no-sub');
    await expect(manageButton.or(noSubHint)).toBeVisible();
    await expect(page.locator('.edit-abo-error')).toHaveCount(0);

    if (await manageButton.isVisible()) {
      // 4. Aufklappen lädt direkt, es gibt keinen "Mein Abo laden"-Knopf mehr
      await page.locator('.edit-abo-header').click();
      await expect(page.locator('.edit-abo-body')).toContainText('admin@statushafen.local');
      await expect(page.locator('.edit-abo-result')).toBeVisible();
      await expect(page.locator('button.btn-abo-lookup')).toHaveCount(0);
    } else {
      await expect(noSubHint).toContainText('noch kein Abonnement');
    }
  });

  // Regressionstest: der "⚙️ Mein Abo bearbeiten"-Button im Abonnieren-CTA rief
  // /api/me/subscription bisher OHNE den erforderlichen ?slug-Parameter auf – das
  // Backend lehnte das immer mit 400 ab, was die Komponente unterschiedslos als
  // "Kein Abonnement gefunden" anzeigte. Der Seed-User "admin" hat laut SeedData()
  // ein Google-Chat-Abo ohne Email auf "Acme Corp" (nur über CreatedByUserID
  // verknüpft) – das deckt den Bug zuverlässig auf, weil hier ein *echtes* Ergebnis
  // erwartet wird statt nur "eine der beiden Meldungen".
  test('CTA-Button "Abonnement verwalten" öffnet den Editor und lädt das echte Abo', async ({ page }) => {
    await page.goto(`http://acme.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://acme.localhost:${PORT}/`);
    await page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' }).click();

    await expect(page.locator('.edit-abo-body')).toBeVisible();
    await expect(page.locator('.edit-abo-result')).toContainText('Google Chat');
    await expect(page.locator('.edit-abo-result')).toContainText('SEED-DEMO');
  });

  // vollständiger Weg über alle vier Zustände. Nutzt bewusst
  // techstart.localhost statt acme.localhost - der Seed-User "admin" hat dort
  // KEIN vorangelegtes Abonnement (siehe SeedData(), nur auf "Acme Corp" liegt
  // das Google-Chat-Demo-Abo). Der Test beendet das selbst angelegte Abo am
  // Ende wieder, der Ausgangszustand (Zustand B) ist also für wiederholte Läufe
  // stabil, ohne auf eine Datenbank-Rücksetzung zwischen E2E-Läufen angewiesen
  // zu sein.
  test('vollständiger Abo-Fluss: kein Abonnement -> abonnieren -> verwalten -> beenden', async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/login`);
    await page.locator('input[type=email]').fill('admin@statushafen.local');
    await page.locator('input[type=password]').fill('admin123');
    await page.locator('button.btn-login').click();
    await expect(page).toHaveURL(/\/admin\/overview/);

    await page.goto(`http://techstart.localhost:${PORT}/`);

    // Zustand B: kein Abonnement, kein Fehler, kein Verwalten-Knopf.
    await expect(page.locator('.subscribe-no-sub')).toBeVisible();
    await expect(page.locator('.edit-abo-error')).toHaveCount(0);
    await expect(page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' })).toHaveCount(0);

    // Abonnieren: E-Mail ist mit der Konto-Adresse
    // vorbelegt, Komponentenauswahl bleibt optional ("alle" als Default).
    await page.locator('button', { hasText: 'Jetzt abonnieren' }).click();
    const wizard = page.locator('.wizard-modal');
    await wizard.locator('.channel-card').first().click();
    await expect(wizard.locator('#subscribe-email')).toHaveValue('admin@statushafen.local');
    await wizard.locator('button', { hasText: 'Weiter' }).click();
    await wizard.locator('button', { hasText: 'Abonnieren' }).click();
    await expect(wizard.locator('.wizard-success')).toBeVisible();
    await wizard.locator('button', { hasText: 'Fertig' }).click();

    // Zustand C: Verwalten-Knopf erscheint, kein Hinweis mehr.
    const manageButton = page.locator('button:not(.user-menu-item)', { hasText: 'Abonnement verwalten' });
    await expect(manageButton).toBeVisible();
    await expect(page.locator('.subscribe-no-sub')).toHaveCount(0);

    // Verwalten öffnen und beenden - Bestätigung läuft auf der Seite, nicht
    // über window.confirm.
    await manageButton.click();
    await expect(page.locator('.edit-abo-result')).toBeVisible();
    await page.locator('button.btn-end-abo').click();
    await expect(page.locator('.edit-abo-end-confirm')).toBeVisible();
    await page.locator('button.btn-end-confirm').click();

    // Zurück in Zustand B.
    await expect(page.locator('.edit-abo-none')).toBeVisible();
    await expect(page.locator('.subscribe-no-sub')).toBeVisible();
    // Der Bereich bleibt samt Kopfzeile stehen, solange der Editor offen ist
    // (SubscriptionManager.vue: noSubscription || showEditAbo); erst nach dem Zuklappen verschwindet er.
    await page.locator('.edit-abo-header').click();
    await expect(manageButton).toHaveCount(0);
  });
});
