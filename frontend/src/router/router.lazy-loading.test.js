import { describe, it, expect, vi } from 'vitest'

// Admin and public views should be loaded via dynamic import() instead of a
// static top-level import, so that Vite creates a separate chunk per view
// instead of packing everything into the initial bundle. An SFC imported
// statically via component: SomeView.vue is an object,
// never a parameterless arrow function that returns a promise when called
// - exactly that reliably distinguishes a () => import(...) lazy loader
// from the former static import.
//
// A separate file instead of a test in router.test.js: vue-router replaces
// route.components.default after the first successful resolution of an
// async component with the resolved component itself (caching). In
// router.test.js earlier tests already navigate to many of these routes,
// so component() would already be replaced there by the resolved component.
// This file imports the router fresh and checks the
// component references before any navigation takes place.
vi.mock('../composables/useAuth', () => ({
  useAuth: vi.fn(() => ({
    isLoggedIn: { value: false },
    token: { value: null },
    setAuth: vi.fn(),
    logout: vi.fn(),
    isGlobalAdmin: { value: false },
  })),
}))

const lazyRoutePaths = [
  '/',
  '/status/:slug',
  '/unsubscribe',
  '/subscribe/confirm',
  '/invitation',
  '/login',
  '/docs/:section?',
  '/auth/sso-callback',
  '/admin/overview',
  '/admin/monitoring',
  '/admin/pages',
  '/admin/components',
  '/admin/incidents',
  '/admin/incidents/:pageId/:id',
  '/admin/maintenance',
  '/admin/notices',
  '/admin/incident-templates',
  '/admin/notification-templates',
  '/admin/subscribers',
  '/admin/audit',
  '/admin/users',
  '/admin/roles',
  '/admin/tags',
  '/admin/languages',
  '/admin/incidents/:incidentId/translations',
  '/admin/components/:componentId/translations',
  '/admin/notices/:noticeId/translations',
  '/admin/sso',
  '/admin/profile',
  '/admin/docs/:section?',
]

describe('Lazy-Loading der Views', () => {
  it.each(lazyRoutePaths)('lädt die View-Komponente für %s dynamisch (Funktion, die ein Promise liefert)', async (path) => {
    const router = (await import('./index')).default
    const record = router.getRoutes().find((r) => r.path === path)
    expect(record, `keine Route mit path ${path} gefunden`).toBeTruthy()

    const component = record.components.default
    expect(typeof component).toBe('function')

    const result = component()
    expect(result).toBeInstanceOf(Promise)
    // Wait for the import: otherwise Vite loads the view only after the test has ended,
    // and a single run of this file ends with
    // EnvironmentTeardownError (exit 1, seen 2026-10-29).
    await expect(result).resolves.toBeTruthy()
  })
})
