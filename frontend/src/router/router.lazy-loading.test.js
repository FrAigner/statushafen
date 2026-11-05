import { describe, it, expect, vi } from 'vitest'

// Admin- und Public-Views sollen per dynamischem import() statt
// statischem Top-Level-Import geladen werden, damit Vite pro View einen
// eigenen Chunk erzeugt statt alles in den initialen Bundle zu packen. Ein
// per component: SomeView.vue statisch importiertes SFC ist ein Objekt,
// niemals eine parameterlose Arrow-Function, die beim Aufruf ein Promise
// liefert - genau das unterscheidet einen () => import(...)-Lazy-Loader
// zuverlässig vom bisherigen statischen Import.
//
// Eigene Datei statt ein Test in router.test.js: vue-router ersetzt
// route.components.default nach der ersten erfolgreichen Auflösung eines
// Async-Components durch die aufgelöste Komponente selbst (Caching). In
// router.test.js navigieren vorherige Tests bereits zu vielen dieser Routes,
// wodurch component() dort schon durch die aufgelöste Komponente ersetzt
// wäre. Diese Datei importiert den Router frisch und prüft die
// Komponenten-Referenzen, bevor irgendeine Navigation stattfindet.
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
    // Den Import abwarten: Sonst lädt Vite die View erst nach dem Testende
    // nach, und ein Einzellauf dieser Datei endet mit
    // EnvironmentTeardownError (Exit 1, gesehen 2026-10-29).
    await expect(result).resolves.toBeTruthy()
  })
})
