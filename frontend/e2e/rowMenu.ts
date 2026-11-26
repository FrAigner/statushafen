import { expect, type Locator, type Page } from '@playwright/test';

// Open the row menu (RowMenu) and pick an entry. If the list reloads once more after saving,
// that closes an already opened menu; so retry until the entry was clickable
// (an already open menu is not toggled again).
export async function clickRowMenuItem(
  page: Page,
  menuButton: Locator,
  name: string | RegExp,
  { exact = false }: { exact?: boolean } = {},
) {
  await expect(async () => {
    if ((await menuButton.getAttribute('aria-expanded')) !== 'true') await menuButton.click();
    await page.getByRole('menuitem', { name, exact }).click({ timeout: 2000 });
  }).toPass({ timeout: 15000 });
}
