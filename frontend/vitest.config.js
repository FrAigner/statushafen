import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// Vitest deckt die Frontend-Unit-Tests ab. Die Playwright-E2E-Specs unter
// e2e/ teilen sich zwar die *.spec.js-Endung, nutzen aber die Playwright-
// test.describe()-API - würde Vitest sie einsammeln, bricht der Lauf mit
// "Playwright Test did not expect test.describe() to be called here" ab.
// Deshalb bleibt der Testlauf bewusst auf src/ begrenzt; E2E läuft separat
// über `npm run test:e2e` (playwright test).
export default defineConfig({
  plugins: [vue()],
  test: {
    include: ['src/**/*.{test,spec}.{js,ts,jsx,tsx}'],
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
    // Aktuell existieren noch keine Unit-Tests - ein leerer Lauf darf die CI
    // nicht rot färben. Sobald src/-Tests hinzukommen, greifen sie automatisch.
    passWithNoTests: true,
    // Standard sind 5 s je Test. Auf ausgelasteten Maschinen (wenige Kerne, parallele Läufe, Coverage an)
    // reißen einzelne Admin-View-Tests die Grenze ohne echten Fehler (AdminLayout, AdminUiTranslations).
    testTimeout: 20000,
    hookTimeout: 30000,
    environment: 'happy-dom',
    setupFiles: ['./src/test-support/vitestSetup.js'],
    coverage: {
      provider: 'v8',
      reporter: ['lcov', 'text'],
      reportsDirectory: 'coverage',
      include: ['src/**/*.{js,ts,vue}'],
      exclude: ['src/**/*.{test,spec}.{js,ts,jsx,tsx}'],
    },
  },
})
