import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import SSOCallback from './SSOCallback.vue'
import { axe } from '../../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// loginWithToken() (useAuth.js) verlangt ein strukturell gültiges JWT
// (3 dot-getrennte Teile, Base64-Payload) - ein beliebiger String würde
// still verworfen (siehe useAuth.test.js).
function makeJwt(payload) {
  const base64url = (obj) =>
    btoa(JSON.stringify(obj)).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '')
  return `${base64url({ alg: 'none', typ: 'JWT' })}.${base64url(payload)}.sig`
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/sso/callback', component: SSOCallback },
      { path: '/login', component: { template: '<div/>' } },
      { path: '/admin', component: { template: '<div/>' } },
      { path: '/admin/profile', component: { template: '<div/>' } },
    ],
  })
}

async function mountCallback(search) {
  vi.stubGlobal('location', { ...window.location, search })
  const router = makeRouter()
  router.push('/sso/callback')
  await router.isReady()

  const wrapper = mount(SSOCallback, { global: { plugins: [i18n, router] } })
  await flushPromises()
  return { wrapper, router }
}

describe('SSOCallback', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    useAuth().logout()
  })

  it('leitet ohne code-Query-Parameter direkt zu /login?error=sso_failed weiter', async () => {
    global.fetch = vi.fn()
    const { router } = await mountCallback('')

    expect(router.currentRoute.value.fullPath).toBe('/login?error=sso_failed')
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('tauscht den code gegen ein Token und leitet bei Erfolg zu /admin weiter', async () => {
    const jwt = makeJwt({ email: 'a@b.de' })
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: jwt }))
    const { router } = await mountCallback('?code=abc123')

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/auth/sso-exchange'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ code: 'abc123' }) })
    )
    expect(useAuth().token.value).toBe(jwt)
    expect(router.currentRoute.value.fullPath).toBe('/admin')
  })

  // Nach dem Verknüpfen im Profil zurück ins Profil; nur dieser
  // feste Wert, kein beliebiges Ziel aus der URL (Open Redirect).
  it('leitet mit next=profile ins Profil weiter', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: makeJwt({ email: 'a@b.de' }) }))
    const { router } = await mountCallback('?code=abc123&next=profile')

    expect(router.currentRoute.value.fullPath).toBe('/admin/profile')
  })

  it('ignoriert unbekannte next-Werte', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: makeJwt({ email: 'a@b.de' }) }))
    const { router } = await mountCallback('?code=abc123&next=https://evil.example')

    expect(router.currentRoute.value.fullPath).toBe('/admin')
  })

  it('leitet bei fehlgeschlagenem Austausch zu /login?error=sso_failed weiter', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'invalid code' }, 400))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { router } = await mountCallback('?code=bad')

    expect(router.currentRoute.value.fullPath).toBe('/login?error=sso_failed')
    expect(useAuth().token.value).toBe('')
  })

  it('hat keine axe-Funde', async () => {
    global.fetch = vi.fn(() => new Promise(() => {}))
    const { wrapper } = await mountCallback('?code=abc')
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
