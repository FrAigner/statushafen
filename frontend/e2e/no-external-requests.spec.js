import { test, expect } from '@playwright/test';

// Port of the Vite dev server (E2E_PORT, default 5173), see playwright.config.js.
const PORT = process.env.E2E_PORT || '5173';

// GDPR: loading sends no requests to third-party hosts
// (e.g. Google Fonts), and the fonts come from our own host.
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
    // Fonts are only loaded when used; trigger them explicitly so the test
    // does not depend on the content currently rendered.
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
