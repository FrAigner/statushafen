import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'

// Mock die useAuth composable
vi.mock('../composables/useAuth', () => ({
  useAuth: vi.fn()
}))

// Importiere den Router nach den Mocks
import router from './index'

describe('Router Navigation Guards', () => {
  let mockIsLoggedIn
  let mockToken
  let mockSetAuth
  let mockLogout
  let mockIsGlobalAdmin
  let fetchMock

  beforeEach(() => {
    mockIsLoggedIn = { value: false }
    mockToken = { value: null }
    mockSetAuth = vi.fn()
    mockLogout = vi.fn()
    mockIsGlobalAdmin = { value: false }

    vi.mocked(useAuth).mockReturnValue({
      isLoggedIn: mockIsLoggedIn,
      token: mockToken,
      setAuth: mockSetAuth,
      logout: mockLogout,
      isGlobalAdmin: mockIsGlobalAdmin,
    })

    fetchMock = vi.fn()
    global.fetch = fetchMock
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Authentifizierungs-Check', () => {
    it('redirectet zu /login wenn nicht angemeldet und Route requiresAuth hat', async () => {
      mockIsLoggedIn.value = false

      await router.push('/admin')

      expect(router.currentRoute.value.path).toBe('/login')
    })

    it('erlaubt Zugriff auf /admin wenn angemeldet', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = true

      await router.push('/admin/overview')

      expect(router.currentRoute.value.path).toBe('/admin/overview')
    })

    it('erlaubt öffentliche Routes ohne Authentifizierung', async () => {
      mockIsLoggedIn.value = false

      await router.push('/status/acme')

      expect(router.currentRoute.value.path).toBe('/status/acme')
    })
  })

  describe('Global-Admin-Check', () => {
    // nicht mehr umleiten, die Route nennt ihr Recht; AdminLayout zeigt „Keine Berechtigung“.
    it('leitet bei globalen Tabs nicht um, sondern nennt das nötige Recht (meta.permission)', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = false

      await router.push('/admin/overview')

      expect(router.currentRoute.value.path).toBe('/admin/overview')
      expect(router.currentRoute.value.meta.permission).toBe('overview:read')
    })

    it('erlaubt Zugriff auf requiresGlobalAdmin Routes wenn Global-Admin', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = true

      await router.push('/admin/overview')

      expect(router.currentRoute.value.path).toBe('/admin/overview')
    })

    it('erlaubt Zugriff auf Seiten-Admin-Routes ohne Global-Admin', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = false

      await router.push('/admin/components')

      expect(router.currentRoute.value.path).toBe('/admin/components')
    })
  })

  describe('Token-Verifikation', () => {
    it('verifiziert Token vom localStorage wenn vorhanden aber isLoggedIn false', async () => {
      mockIsLoggedIn.value = false
      mockToken.value = 'test-jwt-token'
      fetchMock.mockResolvedValue({
        status: 200,
        ok: true,
        json: async () => ({ email: 'user@test.de' })
      })

      await router.push('/admin')
      await new Promise(resolve => setTimeout(resolve, 100))

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining('/api/me'),
        expect.objectContaining({
          headers: expect.objectContaining({
            'X-Auth-Token': 'test-jwt-token'
          })
        })
      )
    })

    it('löscht Auth wenn Verifikation 401 zurückgibt', async () => {
      mockIsLoggedIn.value = false
      mockToken.value = 'invalid-token'
      fetchMock.mockResolvedValue({ status: 401, ok: false })

      await router.push('/admin')
      await new Promise(resolve => setTimeout(resolve, 100))

      expect(mockLogout).toHaveBeenCalled()
    })

    it('löscht Auth wenn Verifikation 403 zurückgibt', async () => {
      mockIsLoggedIn.value = false
      mockToken.value = 'forbidden-token'
      fetchMock.mockResolvedValue({ status: 403, ok: false })

      await router.push('/admin')
      await new Promise(resolve => setTimeout(resolve, 100))

      expect(mockLogout).toHaveBeenCalled()
    })

    it('logged Fehler bei Netzwerkfehler während Verifikation', async () => {
      mockIsLoggedIn.value = false
      mockToken.value = 'test-token'
      fetchMock.mockRejectedValue(new Error('Netzwerkfehler'))
      vi.spyOn(console, 'error').mockImplementation(() => {})

      await router.push('/admin')
      await new Promise(resolve => setTimeout(resolve, 100))

      expect(console.error).toHaveBeenCalledWith(
        expect.stringContaining('Verbindung'),
        expect.any(Error)
      )
    })

    it('logged Warnung bei Server-Fehler während Verifikation', async () => {
      mockIsLoggedIn.value = false
      mockToken.value = 'test-token'
      fetchMock.mockResolvedValue({ status: 500, ok: false })
      vi.spyOn(console, 'warn').mockImplementation(() => {})

      await router.push('/admin')
      await new Promise(resolve => setTimeout(resolve, 100))

      expect(console.warn).toHaveBeenCalledWith(expect.stringContaining('Status 500'))
    })
  })

  describe('Route-Redirects', () => {
    // /admin ist die Startseite (meta.home); AdminLayout führt zum ersten lesbaren Tab.
    it('markiert /admin als Startseite statt fest nach /admin/overview umzuleiten', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = true

      await router.push('/admin')

      expect(router.currentRoute.value.path).toBe('/admin')
      expect(router.currentRoute.value.meta.home).toBe(true)
    })

    it('zeigt Login-View bei /login', async () => {
      await router.push('/login')

      expect(router.currentRoute.value.path).toBe('/login')
    })

    it('zeigt Public-Root bei /', async () => {
      await router.push('/')

      expect(router.currentRoute.value.path).toBe('/')
    })

    it('zeigt SSO-Callback ohne Layout', async () => {
      mockIsLoggedIn.value = true

      await router.push('/auth/sso-callback')

      expect(router.currentRoute.value.path).toBe('/auth/sso-callback')
    })
  })

  describe('Special Routes', () => {
    it('erlaubt Unsubscribe-Link öffentlich', async () => {
      await router.push('/unsubscribe')

      expect(router.currentRoute.value.path).toBe('/unsubscribe')
    })

    // Die Doku ist ohne Login erreichbar, auch einzelne Abschnitte.
    it('erlaubt die Doku-Seite und ihre Abschnitte öffentlich', async () => {
      await router.push('/docs')
      expect(router.currentRoute.value.path).toBe('/docs')

      await router.push('/docs/vorfaelle')
      expect(router.currentRoute.value.path).toBe('/docs/vorfaelle')
      expect(router.currentRoute.value.params.section).toBe('vorfaelle')
    })

    // Bestätigungslink des Double-Opt-in, ohne Login erreichbar.
    it('erlaubt den Abo-Bestätigungslink öffentlich', async () => {
      await router.push('/subscribe/confirm?token=abc')

      expect(router.currentRoute.value.path).toBe('/subscribe/confirm')
    })

    // Einladungslink; die Seite selbst verlangt dann die Anmeldung
    // (mit Rücksprung), der Router leitet nicht vorher auf /login um.
    it('erlaubt den Einladungslink ohne Login', async () => {
      await router.push('/invitation?token=abc')

      expect(router.currentRoute.value.path).toBe('/invitation')
    })

    it('erlaubt Profil-View für alle angemeldeten User (nicht nur Global-Admin)', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = false

      await router.push('/admin/profile')

      expect(router.currentRoute.value.path).toBe('/admin/profile')
    })

    // Seiten-Admins (pages:write) konfigurieren das SSO ihrer Seite;
    // fehlt die Berechtigung, zeigt die View das 403 des Backends.
    it('erlaubt die SSO-Konfiguration für angemeldete User ohne Global-Admin', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = false

      await router.push('/admin/sso')

      expect(router.currentRoute.value.path).toBe('/admin/sso')
    })

    it('erlaubt Sprach-Verwaltung für angemeldete User ohne Global-Admin', async () => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = false

      await router.push('/admin/languages')

      expect(router.currentRoute.value.path).toBe('/admin/languages')
    })

    // Ohne props: true kam die ID aus dem Pfad nicht als Prop
    // in der View an, sie lud `/api/pages/:id/incidents/undefined`.
    it.each([
      ['/admin/incidents/4/translations', 'incidentId', '4'],
      ['/admin/components/23/translations', 'componentId', '23'],
      ['/admin/notices/1/translations', 'noticeId', '1'],
    ])('reicht die ID aus %s als Prop %s an die Übersetzungs-View durch', async (path, prop, id) => {
      mockIsLoggedIn.value = true
      mockIsGlobalAdmin.value = true

      await router.push(path)

      const route = router.currentRoute.value
      expect(route.path).toBe(path)
      const record = route.matched.at(-1)
      expect(record.path).toMatch(/\/translations$/)
      expect(record.props.default).toBe(true)
      expect(route.params[prop]).toBe(id)
    })
  })
})
