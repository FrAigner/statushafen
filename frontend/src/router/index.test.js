import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import router from './index.js'
import { useAuth } from '../composables/useAuth'

describe('router beforeEach guard', () => {
  beforeEach(async () => {
    useAuth().logout()
    localStorage.clear()
    global.fetch = vi.fn()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    // Router ist ein Singleton, dessen Zustand über Tests hinweg besteht - ein
    // Push auf dieselbe Route wie im vorigen Test wäre eine "duplicated
    // navigation" und würde die Guard gar nicht erst erneut ausführen. Daher
    // vor jedem Test auf eine neutrale Route zurücksetzen.
    await router.push('/')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('leitet ohne Login von einer requiresAuth-Route auf /login um', async () => {
    await router.push('/admin/overview')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(global.fetch).not.toHaveBeenCalled()
  })

  // Kein Weiterleiten ins Leere mehr. Die Route nennt ihr Recht (meta.permission),
  // AdminLayout zeigt ohne Leserecht „Keine Berechtigung“ (siehe AdminLayout.test.js).
  it('lässt einen eingeloggten Nicht-Global-Admin auf der Adresse, nennt aber das nötige Recht', async () => {
    useAuth().setAuth('jwt-123', { is_global_admin: false })

    await router.push('/admin/overview')

    expect(router.currentRoute.value.path).toBe('/admin/overview')
    expect(router.currentRoute.value.meta.permission).toBe('overview:read')
  })

  it('lässt einen eingeloggten Global Admin auf eine requiresGlobalAdmin-Route', async () => {
    useAuth().setAuth('jwt-123', { is_global_admin: true })

    await router.push('/admin/overview')

    expect(router.currentRoute.value.path).toBe('/admin/overview')
  })

  it('leitet nicht um, wenn die Zielroute keine Auth-Meta hat', async () => {
    await router.push('/login')

    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('verifiziert ein vorhandenes Token ohne Profil via /api/me und lässt bei Erfolg durch', async () => {
    const { token } = useAuth()
    token.value = 'jwt-from-storage'
    global.fetch.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ is_global_admin: true, email: 'a@b.de' }),
    })

    await router.push('/admin/overview')

    expect(global.fetch).toHaveBeenCalledWith(
      '/api/me',
      expect.objectContaining({ headers: { 'X-Auth-Token': 'jwt-from-storage' } })
    )
    expect(router.currentRoute.value.path).toBe('/admin/overview')
    expect(useAuth().isGlobalAdmin.value).toBe(true)
  })

  it('loggt aus und leitet auf /login um, wenn die Token-Verifizierung 401 liefert', async () => {
    const { token } = useAuth()
    token.value = 'expired-token'
    global.fetch.mockResolvedValue({ ok: false, status: 401 })

    await router.push('/admin/overview')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(useAuth().isLoggedIn.value).toBe(false)
    expect(localStorage.getItem('jwt_token')).toBeNull()
  })

  // Eine 401 von Caddys Basic Auth sagt nichts über das App-Token.
  it('loggt bei einer Basic-Auth-401 der Token-Verifizierung nicht aus', async () => {
    const { token } = useAuth()
    token.value = 'valid-token'
    global.fetch.mockResolvedValue({ ok: false, status: 401, headers: new Headers({ 'WWW-Authenticate': 'Basic realm="x"' }) })

    await router.push('/admin/overview')

    expect(useAuth().token.value).toBe('valid-token')
  })

  it('bleibt ausgeloggt und leitet auf /login um, wenn die Token-Verifizierung fehlschlägt (Netzwerkfehler)', async () => {
    const { token } = useAuth()
    token.value = 'some-token'
    global.fetch.mockRejectedValue(new Error('network down'))

    await router.push('/admin/overview')

    expect(console.error).toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(useAuth().isLoggedIn.value).toBe(false)
  })

  it('loggt bei unerwartetem Serverfehler nur eine Warnung, ohne einzuloggen', async () => {
    const { token } = useAuth()
    token.value = 'some-token'
    global.fetch.mockResolvedValue({ ok: false, status: 500 })

    await router.push('/admin/overview')

    expect(console.warn).toHaveBeenCalled()
    expect(useAuth().isLoggedIn.value).toBe(false)
    expect(router.currentRoute.value.path).toBe('/login')
  })
})
