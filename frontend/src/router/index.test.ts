import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import router from './index'
import { useAuth } from '../composables/useAuth'
import { asResponse, mockedFetch } from '../test-support/fetchStub'

describe('router beforeEach guard', () => {
  beforeEach(async () => {
    useAuth().logout()
    localStorage.clear()
    global.fetch = vi.fn()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    // The router is a singleton whose state persists across tests - a
    // push to the same route as in the previous test would be a "duplicated
    // navigation" and would not even run the guard again. Hence reset to a
    // neutral route before every test.
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

  // A direct visit of a requiresAuth route counts as an explicit choice: back there after sign-in.
  it('passes the requested route as returnTo to the login page', async () => {
    await router.push('/admin/incidents?status=open')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.returnTo).toBe('/admin/incidents?status=open')
  })

  // Already signed in on /login?returnTo=...: go straight to the (checked) target.
  it.each([
    ['status page', '/status/acme', '/status/acme'],
    ['admin area', '/admin/overview', '/admin/overview'],
    ['protocol-relative', '//angreifer.example', '/'],
    ['absolute URL', 'https://angreifer.example', '/'],
    ['unknown route', '/admin/gibt/es/nicht/wirklich', '/'],
  ])('sends a signed-in user from /login straight to returnTo (%s)', async (_, returnTo, expected) => {
    useAuth().setAuth('jwt-123', { is_global_admin: true })

    await router.push({ path: '/login', query: { returnTo } })

    expect(router.currentRoute.value.fullPath).toBe(expected)
  })

  it('leaves a signed-in user on /login without returnTo', async () => {
    useAuth().setAuth('jwt-123', { is_global_admin: true })

    await router.push('/login')

    expect(router.currentRoute.value.fullPath).toBe('/login')
  })

  // No more redirecting into nowhere. The route names its permission (meta.permission);
  // without read permission AdminLayout shows "no permission" (see AdminLayout.test.ts).
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
    mockedFetch().mockResolvedValue(asResponse({
      ok: true,
      status: 200,
      json: async () => ({ is_global_admin: true, email: 'a@b.de' }),
    }))

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
    mockedFetch().mockResolvedValue(asResponse({ ok: false, status: 401 }))

    await router.push('/admin/overview')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(useAuth().isLoggedIn.value).toBe(false)
    expect(localStorage.getItem('jwt_token')).toBeNull()
  })

  // A 401 from Caddy's basic auth says nothing about the app token.
  it('loggt bei einer Basic-Auth-401 der Token-Verifizierung nicht aus', async () => {
    const { token } = useAuth()
    token.value = 'valid-token'
    mockedFetch().mockResolvedValue(asResponse({ ok: false, status: 401, headers: new Headers({ 'WWW-Authenticate': 'Basic realm="x"' }) }))

    await router.push('/admin/overview')

    expect(useAuth().token.value).toBe('valid-token')
  })

  it('bleibt ausgeloggt und leitet auf /login um, wenn die Token-Verifizierung fehlschlägt (Netzwerkfehler)', async () => {
    const { token } = useAuth()
    token.value = 'some-token'
    mockedFetch().mockRejectedValue(new Error('network down'))

    await router.push('/admin/overview')

    expect(console.error).toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(useAuth().isLoggedIn.value).toBe(false)
  })

  it('loggt bei unerwartetem Serverfehler nur eine Warnung, ohne einzuloggen', async () => {
    const { token } = useAuth()
    token.value = 'some-token'
    mockedFetch().mockResolvedValue(asResponse({ ok: false, status: 500 }))

    await router.push('/admin/overview')

    expect(console.warn).toHaveBeenCalled()
    expect(useAuth().isLoggedIn.value).toBe(false)
    expect(router.currentRoute.value.path).toBe('/login')
  })
})
