import { test, expect } from '@playwright/test';

// Port des Vite-Dev-Servers (E2E_PORT, Standard 5173), siehe playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// DSGVO: Beim Laden gehen keine Requests an fremde Hosts
// (z. B. Google Fonts), und die Schriften kommen vom eigenen Host.
const OWN_ORIGIN = `http://localhost:${PORT}`;

for (const path of ['/', '/login']) {
  test(`keine Fremd-Requests beim Laden von ${path}`, async ({ page }) => {
    const foreign = [];
    const fonts = [];
    page.on('response', (res) => {
      if (res.url().endsWith('.woff2')) fonts.push({ url: res.url(), status: res.status() });
    });
    page.on('request', (req) => {
      const url = req.url();
      if (url.startsWith('data:') || url.startsWith('blob:')) return;
      if (new URL(url).origin !== OWN_ORIGIN) foreign.push(url);
    });

    await page.goto(`${OWN_ORIGIN}${path}`, { waitUntil: 'networkidle' });
    // Schriften werden erst bei Verwendung geladen; explizit anstoßen, damit
    // der Test unabhängig vom gerade gerenderten Inhalt ist.
    await page.evaluate(() =>
      Promise.all(
        ['Inter', 'IBM Plex Sans', 'IBM Plex Mono'].flatMap((f) =>
          [400, 600].map((w) => document.fonts.load(`${w} 1em "${f}"`)),
        ),
      ),
    );

    expect(foreign).toEqual([]);
    expect(fonts.length).toBeGreaterThan(0);
    for (const f of fonts) {
      expect(f.url.startsWith(OWN_ORIGIN)).toBe(true);
      expect(f.status).toBe(200);
    }
  });
}
