import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { defineComponent, h, ref } from 'vue'
import SSOLinks from './SSOLinks.vue'
import { useAutoDismissAlerts, ALERT_AUTO_DISMISS_MS } from '../composables/useAutoDismissAlerts'
import { stubConfirm } from '../test-support/confirm'

// Profilbereich „Mit SSO verknüpfen“ (POST /api/auth/oidc/link)
// samt Anzeige und Lösen bestehender Verknüpfungen (GET/DELETE /api/me/sso).

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

const LIST = {
  identities: [
    { id: 4, status_page_id: 1, page_name: 'Acme', email: 'erika@idp.example', created_at: '2026-10-01T10:00:00Z' },
    { id: 5, status_page_id: 9, page_name: '', email: '', created_at: '2026-10-02T10:00:00Z' },
  ],
  linkable: [{ status_page_id: 2, page_name: 'Beta' }],
}

function makeFetch(handlers = {}) {
  return vi.fn(async (url, options = {}) => {
    const key = `${options.method || 'GET'} ${url}`
    const handler = handlers[key]
    if (!handler) throw new Error(`Unerwarteter Fetch-Aufruf: ${key}`)
    return typeof handler === 'function' ? handler(options) : handler
  })
}

let activeWrapper = null

async function mountLinks({ fetchImpl, search = '' } = {}) {
  vi.stubGlobal('location', { ...window.location, search, assign: vi.fn() })
  global.fetch = fetchImpl
  const wrapper = mount(SSOLinks, { global: { plugins: [i18n] } })
  activeWrapper = wrapper
  await flushPromises()
  return wrapper
}

describe('SSOLinks', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().setAuth('jwt-1', { email: 'a@b.de' })
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    activeWrapper?.unmount()
    activeWrapper = null
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('zeigt bestehende Verknüpfungen und verknüpfbare Seiten', async () => {
    const wrapper = await mountLinks({ fetchImpl: makeFetch({ 'GET /api/me/sso': jsonResponse(LIST) }) })

    const linked = wrapper.findAll('[data-test="sso-linked"]')
    expect(linked).toHaveLength(2)
    expect(linked[0].text()).toContain('Acme')
    expect(linked[0].text()).toContain('als erika@idp.example')
    expect(linked[1].text()).toContain('Gelöschte Statusseite')
    expect(linked[1].text()).not.toContain('als')
    const linkable = wrapper.findAll('[data-test="sso-linkable"]')
    expect(linkable).toHaveLength(1)
    expect(linkable[0].text()).toContain('Beta')
  })

  it('bleibt ohne Verknüpfungen und verknüpfbare Seiten ausgeblendet', async () => {
    const wrapper = await mountLinks({
      fetchImpl: makeFetch({ 'GET /api/me/sso': jsonResponse({ identities: [], linkable: [] }) }),
    })
    expect(wrapper.find('section').exists()).toBe(false)
  })

  it('zeigt einen Ladefehler', async () => {
    const wrapper = await mountLinks({ fetchImpl: makeFetch({ 'GET /api/me/sso': jsonResponse({}, 500) }) })
    expect(wrapper.find('[role="alert"]').text()).toBe('SSO-Verknüpfungen konnten nicht geladen werden.')
  })

  it('startet die Verknüpfung und leitet zum Anbieter weiter', async () => {
    const fetchImpl = makeFetch({
      'GET /api/me/sso': jsonResponse(LIST),
      'POST /api/auth/oidc/link?page=2': jsonResponse({ url: 'https://idp.example/authorize?x=1' }),
    })
    const wrapper = await mountLinks({ fetchImpl })

    await wrapper.find('[data-test="sso-linkable"] button').trigger('click')
    await flushPromises()

    expect(window.location.assign).toHaveBeenCalledWith('https://idp.example/authorize?x=1')
  })

  it.each(['javascript:alert(1)', 'data:text/html,x', 'http://idp.example/authorize'])(
    'folgt keiner unsicheren Authorize-URL (%s)',
    async (url) => {
      vi.stubEnv('DEV', false)
      const fetchImpl = makeFetch({
        'GET /api/me/sso': jsonResponse(LIST),
        'POST /api/auth/oidc/link?page=2': jsonResponse({ url }),
      })
      const wrapper = await mountLinks({ fetchImpl })

      await wrapper.find('[data-test="sso-linkable"] button').trigger('click')
      await flushPromises()

      expect(window.location.assign).not.toHaveBeenCalled()
      expect(wrapper.find('[role="alert"]').text()).toContain('unsichere Adresse')
    }
  )

  it('zeigt einen Fehler, wenn die Verknüpfung nicht startet', async () => {
    const fetchImpl = makeFetch({
      'GET /api/me/sso': jsonResponse(LIST),
      'POST /api/auth/oidc/link?page=2': jsonResponse({ error: 'OIDC-Anbieter nicht erreichbar' }, 502),
    })
    const wrapper = await mountLinks({ fetchImpl })

    await wrapper.find('[data-test="sso-linkable"] button').trigger('click')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toContain('OIDC-Anbieter nicht erreichbar')
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('löst nach Bestätigung eine Verknüpfung und lädt neu', async () => {
    let loads = 0
    const fetchImpl = makeFetch({
      'GET /api/me/sso': () => jsonResponse(loads++ === 0 ? LIST : { identities: [], linkable: LIST.linkable }),
      'DELETE /api/me/sso/4': { ok: true, status: 204, json: async () => null },
    })
    const confirm = stubConfirm(true)
    const wrapper = await mountLinks({ fetchImpl })

    await wrapper.find('[data-test="sso-linked"] button').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith('Verknüpfung mit „Acme“ wirklich lösen?')
    expect(wrapper.findAll('[data-test="sso-linked"]')).toHaveLength(0)
    expect(wrapper.find('[role="status"]').text()).toBe('Verknüpfung gelöst.')
  })

  it('löst ohne Bestätigung nichts', async () => {
    const fetchImpl = makeFetch({ 'GET /api/me/sso': jsonResponse(LIST) })
    const confirm = stubConfirm(false)
    const wrapper = await mountLinks({ fetchImpl })

    await wrapper.findAll('[data-test="sso-linked"] button')[1].trigger('click')

    expect(confirm).toHaveBeenCalledWith('Verknüpfung mit „Gelöschte Statusseite“ wirklich lösen?')
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })

  it('meldet einen Fehler beim Lösen', async () => {
    const fetchImpl = makeFetch({
      'GET /api/me/sso': jsonResponse(LIST),
      'DELETE /api/me/sso/4': jsonResponse({ error: 'kaputt' }, 500),
    })
    stubConfirm(true)
    const wrapper = await mountLinks({ fetchImpl })

    await wrapper.find('[data-test="sso-linked"] button').trigger('click')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toBe('Verknüpfung konnte nicht gelöst werden.')
    expect(wrapper.findAll('[data-test="sso-linked"]')).toHaveLength(2)
  })

  it('zeigt einen Fehlercode aus dem Callback übersetzt an', async () => {
    const wrapper = await mountLinks({
      fetchImpl: makeFetch({ 'GET /api/me/sso': jsonResponse({ identities: [], linkable: [] }) }),
      search: '?oidc_error=already_linked',
    })
    expect(wrapper.find('[role="alert"]').text()).toBe('Diese SSO-Anmeldung ist bereits mit einem anderen Konto verknüpft.')
  })

  it('zeigt für unbekannte Codes die allgemeine Meldung', async () => {
    const wrapper = await mountLinks({
      fetchImpl: makeFetch({ 'GET /api/me/sso': jsonResponse({ identities: [], linkable: [] }) }),
      search: '?oidc_error=quatsch',
    })
    expect(wrapper.find('[role="alert"]').text()).toBe('Die SSO-Anmeldung ist fehlgeschlagen.')
  })

  // Verknüpfungen und verknüpfbare Seiten tragen ihr Protokoll.
  describe('SAML', () => {
    const SAML_LIST = {
      identities: [{ id: 6, status_page_id: 1, page_name: 'Acme', protocol: 'saml', email: '', created_at: '2026-10-03T10:00:00Z' }],
      linkable: [
        { status_page_id: 3, page_name: 'Gamma', protocol: 'oidc' },
        { status_page_id: 3, page_name: 'Gamma', protocol: 'saml' },
      ],
    }

    it('zeigt das Protokoll an Verknüpfung und verknüpfbarer Seite', async () => {
      const wrapper = await mountLinks({ fetchImpl: makeFetch({ 'GET /api/me/sso': jsonResponse(SAML_LIST) }) })

      expect(wrapper.find('[data-test="sso-linked"]').text()).toContain('SAML')
      const linkable = wrapper.findAll('[data-test="sso-linkable"]')
      expect(linkable).toHaveLength(2)
      expect(linkable[0].text()).toContain('OIDC')
      expect(linkable[1].text()).toContain('SAML')
    })

    it('startet die SAML-Verknüpfung über den SAML-Endpunkt', async () => {
      const fetchImpl = makeFetch({
        'GET /api/me/sso': jsonResponse(SAML_LIST),
        'POST /api/auth/saml/link?page=3': jsonResponse({ url: 'https://idp.example/sso?SAMLRequest=x' }),
      })
      const wrapper = await mountLinks({ fetchImpl })

      await wrapper.findAll('[data-test="sso-linkable"] button')[1].trigger('click')
      await flushPromises()

      expect(window.location.assign).toHaveBeenCalledWith('https://idp.example/sso?SAMLRequest=x')
    })

    it('nimmt ohne Protokollangabe OIDC (ältere Antworten)', async () => {
      const fetchImpl = makeFetch({
        'GET /api/me/sso': jsonResponse(LIST),
        'POST /api/auth/oidc/link?page=2': jsonResponse({ url: 'https://idp.example/authorize' }),
      })
      const wrapper = await mountLinks({ fetchImpl })

      expect(wrapper.find('[data-test="sso-linkable"]').text()).toContain('OIDC')
      await wrapper.find('[data-test="sso-linkable"] button').trigger('click')
      await flushPromises()
      expect(window.location.assign).toHaveBeenCalledWith('https://idp.example/authorize')
    })
  })

  // Review: Die automatische Ausblendung klickt auf das Schließen-Kreuz; ohne Kreuz blieb die Meldung stehen.
  it('lässt die Erfolgsmeldung nach der Frist im Admin-Inhaltsbereich tatsächlich verschwinden und hat ein Schließen-Kreuz', async () => {
    const fetchImpl = makeFetch({
      'GET /api/me/sso': () => jsonResponse(LIST),
      'DELETE /api/me/sso/4': { ok: true, status: 204, json: async () => null },
    })
    stubConfirm(true)
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      vi.stubGlobal('location', { ...window.location, search: '', assign: vi.fn() })
      global.fetch = fetchImpl
      const Host = defineComponent({
        setup() {
          const el = ref(null)
          useAutoDismissAlerts(el)
          return () => h('main', { ref: el, class: 'admin-content' }, [h(SSOLinks)])
        },
      })
      const wrapper = mount(Host, { global: { plugins: [i18n] }, attachTo: document.body })
      activeWrapper = wrapper
      await flushPromises()
      await wrapper.find('[data-test="sso-linked"] button').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-success').text()).toBe('Verknüpfung gelöst.')
      expect(wrapper.find('.alert-success .alert-close').attributes('aria-label')).toBe('Meldung schließen')

      vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS + 10)
      await flushPromises()
      expect(wrapper.find('.alert-success').exists()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('schließt die Erfolgsmeldung per Kreuz', async () => {
    const fetchImpl = makeFetch({
      'GET /api/me/sso': () => jsonResponse(LIST),
      'DELETE /api/me/sso/4': { ok: true, status: 204, json: async () => null },
    })
    stubConfirm(true)
    const wrapper = await mountLinks({ fetchImpl })
    await wrapper.find('[data-test="sso-linked"] button').trigger('click')
    await flushPromises()
    await wrapper.find('.alert-success .alert-close').trigger('click')
    expect(wrapper.find('.alert-success').exists()).toBe(false)
  })
})
