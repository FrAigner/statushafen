import { expect } from '@playwright/test';

// Zeilenmenü (RowMenu) öffnen und einen Eintrag wählen. Lädt die Liste nach dem Speichern
// noch einmal nach, schließt das ein schon geöffnetes Menü; deshalb so lange wiederholen, bis der
// Eintrag klickbar war (ein bereits offenes Menü wird nicht erneut umgeschaltet).
export async function clickRowMenuItem(page, menuButton, name, { exact = false } = {}) {
  await expect(async () => {
    if ((await menuButton.getAttribute('aria-expanded')) !== 'true') await menuButton.click();
    await page.getByRole('menuitem', { name, exact }).click({ timeout: 2000 });
  }).toPass({ timeout: 15000 });
}
