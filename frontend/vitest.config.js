import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// Vitest covers the frontend unit tests. The Playwright e2e specs under e2e/ share the *.spec.js
// suffix but use Playwright's test.describe() API; if Vitest collected them, the run would abort with
// "Playwright Test did not expect test.describe() to be called here". The test run is therefore
// deliberately limited to src/; e2e runs separately via `npm run test:e2e` (playwright test).
export default defineConfig({
  plugins: [vue()],
  test: {
    include: ['src/**/*.{test,spec}.{js,ts,jsx,tsx}'],
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
    // An empty run (e.g. a filter that matches no test) must not fail CI.
    passWithNoTests: true,
    // The default is 5 s per test. On busy machines (few cores, parallel runs, coverage on) single admin
    // view tests exceed that limit without a real failure (AdminLayout, AdminUiTranslations).
    testTimeout: 20000,
    hookTimeout: 30000,
    environment: 'happy-dom',
    setupFiles: ['./src/test-support/vitestSetup.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['lcov', 'text'],
      reportsDirectory: 'coverage',
      // Application code is TypeScript (.ts, .vue); .js stays listed so that a stray JavaScript file
      // would still show up as uncovered instead of silently missing from the report.
      include: ['src/**/*.{js,ts,vue}'],
      exclude: ['src/**/*.{test,spec}.{js,ts,jsx,tsx}'],
    },
  },
})
