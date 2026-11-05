import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import OidcLoginButton from './OidcLoginButton.vue'

// Knopf „Mit SSO anmelden“ für Seiten mit aktivem OIDC.

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

async function mountButton(slug, response) {
  global.fetch = vi.fn().mockResolvedValue(response)
  const wrapper = mount(OidcLoginButton, { props: { slug }, global: { plugins: [i18n] } })
  await flushPromises()
  return wrapper
}

describe('OidcLoginButton', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
  })

  it('zeigt bei aktivem OIDC einen Link auf den Login-Start der Seite', async () => {
    const wrapper = await mountButton('acme', jsonResponse({ oidc_enabled: true, page_id: 3 }))

    expect(global.fetch).toHaveBeenCalledWith('/api/public/pages/acme/sso', expect.anything())
    const link = wrapper.find('a.oidc-login-btn')
    expect(link.attributes('href')).toBe('/api/auth/oidc/login?page=3')
    expect(link.text()).toContain('Mit SSO anmelden')
  })

  it('nutzt VITE_API_URL als Basis und kodiert den Slug', async () => {
    vi.stubEnv('VITE_API_URL', 'https://api.example.org')
    const wrapper = await mountButton('a/b', jsonResponse({ oidc_enabled: true, page_id: 9 }))

    expect(global.fetch).toHaveBeenCalledWith('https://api.example.org/api/public/pages/a%2Fb/sso', expect.anything())
    expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('https://api.example.org/api/auth/oidc/login?page=9')
  })

  it('bleibt ohne aktives OIDC unsichtbar', async () => {
    const wrapper = await mountButton('acme', jsonResponse({ oidc_enabled: false, page_id: 3 }))
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('bleibt bei Fehlern der Abfrage unsichtbar', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = await mountButton('acme', jsonResponse({ error: 'nicht gefunden' }, 404))
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('fragt ohne Slug nichts ab', async () => {
    const wrapper = await mountButton('', jsonResponse({ oidc_enabled: true, page_id: 3 }))
    expect(global.fetch).not.toHaveBeenCalled()
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('fragt bei einem neuen Slug erneut ab', async () => {
    const wrapper = await mountButton('acme', jsonResponse({ oidc_enabled: false, page_id: 3 }))
    global.fetch.mockResolvedValue(jsonResponse({ oidc_enabled: true, page_id: 4 }))

    await wrapper.setProps({ slug: 'beta' })
    await flushPromises()

    expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('/api/auth/oidc/login?page=4')
  })

  // zweiter Knopf für SAML, beide in einem gemeinsamen Element.
  it('zeigt bei aktivem SAML einen Link auf den SAML-Login-Start', async () => {
    const wrapper = await mountButton('acme', jsonResponse({ oidc_enabled: false, saml_enabled: true, page_id: 3 }))

    expect(wrapper.find('a.oidc-login-btn').exists()).toBe(false)
    const link = wrapper.find('a.saml-login-btn')
    expect(link.attributes('href')).toBe('/api/auth/saml/login?page=3')
    expect(link.text()).toContain('Mit SSO anmelden (SAML)')
  })

  it('zeigt mit OIDC und SAML beide Knöpfe', async () => {
    const wrapper = await mountButton('acme', jsonResponse({ oidc_enabled: true, saml_enabled: true, page_id: 3 }))

    expect(wrapper.findAll('.sso-login-buttons a')).toHaveLength(2)
    expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('/api/auth/oidc/login?page=3')
    expect(wrapper.find('a.saml-login-btn').attributes('href')).toBe('/api/auth/saml/login?page=3')
  })

  it('gibt Attribute wie class an das gemeinsame Element weiter', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ oidc_enabled: true, saml_enabled: false, page_id: 3 }))
    const wrapper = mount(OidcLoginButton, { props: { slug: 'acme' }, attrs: { class: 'extra' }, global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.find('.sso-login-buttons').classes()).toContain('extra')
  })

  // Private Seiten nennen Seiten-ID und SSO-Art nur Berechtigten: Für alle anderen
  // antwortet die Auskunft mit 404 wie bei einer fehlenden Seite. Mit `fallback`
  // bietet der Knopf dann den Einstieg über den Slug an.
  describe('Rückfall über den Slug (fallback)', () => {
    async function mountFallback(slug, response) {
      global.fetch = vi.fn().mockResolvedValue(response)
      const wrapper = mount(OidcLoginButton, { props: { slug, fallback: true }, global: { plugins: [i18n] } })
      await flushPromises()
      return wrapper
    }

    it('zeigt bei 404 der Auskunft einen Link auf den Einstieg über den Slug', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const wrapper = await mountFallback('geheim', jsonResponse({ code: 'page_not_found' }, 404))

      const link = wrapper.find('a.oidc-login-btn')
      expect(link.attributes('href')).toBe('/api/auth/sso/login?slug=geheim')
      expect(link.text()).toContain('Mit SSO anmelden')
    })

    it('kodiert den Slug und nutzt VITE_API_URL', async () => {
      vi.stubEnv('VITE_API_URL', 'https://api.example.org')
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const wrapper = await mountFallback('a/b', jsonResponse({}, 404))
      expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('https://api.example.org/api/auth/sso/login?slug=a%2Fb')
    })

    it('zeigt bei einer Seite ohne SSO keinen Knopf', async () => {
      const wrapper = await mountFallback('acme', jsonResponse({ oidc_enabled: false, saml_enabled: false, page_id: 3 }))
      expect(wrapper.find('a').exists()).toBe(false)
    })

    it('zeigt bei erfolgreicher Auskunft die üblichen Knöpfe statt des Rückfalls', async () => {
      const wrapper = await mountFallback('acme', jsonResponse({ oidc_enabled: true, saml_enabled: true, page_id: 3 }))
      expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('/api/auth/oidc/login?page=3')
      expect(wrapper.find('a.saml-login-btn').attributes('href')).toBe('/api/auth/saml/login?page=3')
    })

    it('bleibt bei anderen Fehlern als 404 unsichtbar', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const wrapper = await mountFallback('acme', jsonResponse({}, 500))
      expect(wrapper.find('a').exists()).toBe(false)
    })

    it('bietet den Rückfall nicht an, wenn die Seite gewechselt wird und die neue Auskunft passt', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const wrapper = await mountFallback('geheim', jsonResponse({}, 404))
      expect(wrapper.find('a').exists()).toBe(true)
      global.fetch.mockResolvedValue(jsonResponse({ oidc_enabled: false, saml_enabled: false, page_id: 4 }))
      await wrapper.setProps({ slug: 'offen' })
      await flushPromises()
      expect(wrapper.find('a').exists()).toBe(false)
    })
  })
})
