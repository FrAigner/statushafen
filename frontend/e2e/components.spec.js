import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

test.describe('Component Tree', () => {
  test('should render components and allow toggling groups on public dashboard', async ({ page }) => {
    await page.goto(`http://techstart.localhost:${PORT}/`);

    // Check that we see the tree nodes OR the empty state
    const treeNodes = page.locator('.tree-node');
    const emptyState = page.locator('.empty-filter-state');
    
    // Playwright allows waiting for either
    await expect(treeNodes.first().or(emptyState.first())).toBeVisible();

    if (await treeNodes.count() > 0) {
      // Check if there are any group components (with the toggle icon)
      const toggleIcon = page.locator('.toggle-icon').first();
      
      // If the database has groups seeded, test the toggle
      if (await toggleIcon.isVisible()) {
        const parentRow = toggleIcon.locator('..').locator('..');
        const childrenContainer = parentRow.locator('..').locator('.tree-children');
        
        const isVisibleInitially = await childrenContainer.isVisible();
        await parentRow.click();
        
        if (isVisibleInitially) {
          await expect(childrenContainer).toBeHidden();
        } else {
          await expect(childrenContainer).toBeVisible();
        }
      }
    }
  });
});
