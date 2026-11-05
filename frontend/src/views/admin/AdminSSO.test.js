import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminSSO from './AdminSSO.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'
import { setConfirmHandler } from '../../composables/useConfirm'

// Die View sprach früher /api/pages/:id/sso ohne Auth-Header mit
// camelCase-Feldern an und bezog die Seite aus einem Routen-Parameter, den
// /admin/sso gar nicht hat. Sie nutzt jetzt wie die übrigen Admin-Views die
// aktive Statusseite aus useTenant und die oidc_*-Felder des Backends.

const SSO_URL = '/api/admin/pages/7/sso'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function ssoFixture(overrides = {}) {
  return {
    status_page_id: 7,
    oidc_enabled: true,
    oidc_discovery_url: 'https://idp.example/.well-known/openid-configuration',
    oidc_client_id: 'client-1',
    oidc_scopes: 'openid email profile',
    oidc_attribute_email: 'email',
    oidc_attribute_name: 'name',
    oidc_attribute_role: 'groups',
    oidc_acs_url: 'http://localhost:8080/api/auth/oidc/callback?page=7',
    test_login_enabled: false,
    oidc_client_secret_set: true,
    // SAML
    saml_enabled: false,
    saml_idp_metadata_url: 'https://idp.example/saml/metadata',
    saml_idp_metadata_xml: '',
    saml_attribute_email: 'email',
    saml_attribute_name: 'name',
    saml_allow_unspecified_nameid: false,
    saml_sp_certificate: '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n',
    saml_entity_id: 'http://localhost:8080/api/auth/saml/7/metadata',
    saml_metadata_url: 'http://localhost:8080/api/auth/saml/7/metadata',
    saml_acs_url: 'http://localhost:8080/api/auth/saml/7/acs',
    ...overrides,
  }
}

// Antwortet je Methode; Fehler werden als { status, body } übergeben.
function makeFetch({ get = jsonResponse(ssoFixture()), put, del, test, samlTest } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (url === `${SSO_URL}/test` && method === 'POST') return test()
    if (url === `${SSO_URL}/saml-test` && method === 'POST') return samlTest()
    if (url !== SSO_URL) throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
    if (method === 'GET') return typeof get === 'function' ? get() : get
    if (method === 'PUT') return put(JSON.parse(options.body))
    if (method === 'DELETE') return del()
    throw new Error(`Unerwartete Methode ${method}`)
  })
}

let activeWrapper = null

async function mountAdminSSO({ fetchImpl = makeFetch(), tenantId = '7', search = '' } = {}) {
  vi.stubGlobal('location', { ...window.location, search, assign: vi.fn() })
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl
  const wrapper = mount(AdminSSO, { global: { plugins: [i18n], stubs: onyxFormStubs } })
  activeWrapper = wrapper
  await flushPromises()
  return wrapper
}

describe('AdminSSO.vue', () => {
  beforeEach(() => {
    useAuth().logout()
    localStorage.clear()
    useAuth().setAuth('tok-123', { is_global_admin: true })
    useTenant().setActiveTenant(null)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // Tests assertieren auf den deutschen Wortlaut übersetzter Texte;
    // ohne gespeicherte Präferenz würde detectInitialLocale() in der Testumgebung
    // (happy-dom navigator.language === 'en-US') sonst auf 'en' fallen.
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    activeWrapper?.unmount()
    activeWrapper = null
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('rendert die übersetzte SSO-Konfigurationsseite statt roher i18n-Keys', async () => {
    const wrapper = await mountAdminSSO()

    expect(wrapper.find('h2').text()).toContain('SSO Konfiguration')
    expect(wrapper.text()).not.toContain('admin.sso.')
  })

  it('rendert die Seite auf Englisch übersetzt, wenn die UI-Sprache Englisch ist', async () => {
    i18n.global.locale.value = 'en'
    const wrapper = await mountAdminSSO()

    expect(wrapper.find('h2').text()).toContain('SSO Configuration')
    expect(wrapper.text()).toContain('Stored – leave empty to keep it')
    expect(wrapper.text()).not.toContain('admin.sso.')
  })

  // Stufe 4d: Titel als h2 (die Topbar trägt die h1), Felder über
  // FormField: Pflicht-Stern + required nur bei aktivem OIDC, Hinweis per aria-describedby.
  it('rendert den Titel als h2 ohne h1 und verbindet Hinweise per aria-describedby', async () => {
    const wrapper = await mountAdminSSO()

    expect(wrapper.find('h2.page-head__title').exists()).toBe(true)
    expect(wrapper.find('h1').exists()).toBe(false)
    const scopes = wrapper.find('#sso-scopes')
    expect(wrapper.find(`#${scopes.attributes('aria-describedby')}`).text()).toContain('Durch Leerzeichen getrennt')
    expect(wrapper.find('#sso-client-id').attributes('required')).toBeDefined()
    expect(wrapper.findAll('.form-field__required').length).toBeGreaterThan(0)
  })

  it('verlangt keine Pflichtfelder, solange OIDC ausgeschaltet ist', async () => {
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ oidc_enabled: false })) }) })

    expect(wrapper.find('#sso-client-id').attributes('required')).toBeUndefined()
    expect(wrapper.find('.form-field__required').exists()).toBe(false)
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = await mountAdminSSO()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('lädt die Config der aktiven Statusseite mit Auth-Header und füllt das Formular', async () => {
    const fetchImpl = makeFetch()
    const wrapper = await mountAdminSSO({ fetchImpl })

    expect(fetchImpl).toHaveBeenCalledWith(SSO_URL, expect.objectContaining({
      headers: expect.objectContaining({ 'X-Auth-Token': 'tok-123' }),
    }))
    expect(wrapper.find('#sso-enabled').element.checked).toBe(true)
    expect(wrapper.find('#sso-discovery-url').element.value).toBe('https://idp.example/.well-known/openid-configuration')
    expect(wrapper.find('#sso-client-id').element.value).toBe('client-1')
    expect(wrapper.find('#sso-scopes').element.value).toBe('openid email profile')
    expect(wrapper.find('#sso-attribute-email').element.value).toBe('email')
    expect(wrapper.find('#sso-attribute-name').element.value).toBe('name')
    // Rollen-Mapping aus einem Claim wird nicht unterstützt.
    expect(wrapper.find('#sso-attribute-role').exists()).toBe(false)
    expect(wrapper.find('#sso-test-login').element.checked).toBe(false)
    expect(wrapper.find('#sso-acs-url').element.value).toBe('http://localhost:8080/api/auth/oidc/callback?page=7')
  })

  it('zeigt bei gespeichertem Secret „leer lassen zum Beibehalten“ und verlangt es nicht', async () => {
    const wrapper = await mountAdminSSO()

    const secret = wrapper.find('#sso-client-secret')
    expect(secret.element.value).toBe('')
    expect(secret.attributes('required')).toBeUndefined()
    expect(wrapper.text()).toContain('Gesetzt – leer lassen zum Beibehalten')
  })

  it('verlangt das Secret, solange keins gespeichert ist und OIDC aktiv ist', async () => {
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ oidc_client_secret_set: false })) }) })

    expect(wrapper.find('#sso-client-secret').attributes('required')).toBeDefined()
    expect(wrapper.text()).not.toContain('Gesetzt – leer lassen zum Beibehalten')

    await wrapper.find('#sso-enabled').setValue(false)
    expect(wrapper.find('#sso-client-secret').attributes('required')).toBeUndefined()
    expect(wrapper.find('#sso-discovery-url').attributes('required')).toBeUndefined()
    expect(wrapper.find('#sso-client-id').attributes('required')).toBeUndefined()
  })

  it('speichert per PUT mit den oidc_*-Feldern und leert danach das Secret-Feld', async () => {
    const put = vi.fn(async (body) => jsonResponse(ssoFixture({ oidc_client_id: body.oidc_client_id, test_login_enabled: body.test_login_enabled })))
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ put }) })

    await wrapper.find('#sso-client-id').setValue('client-2')
    await wrapper.find('#sso-scopes').setValue('openid')
    await wrapper.find('#sso-attribute-email').setValue('mail')
    await wrapper.find('#sso-attribute-name').setValue('anzeigename')
    await wrapper.find('#sso-client-secret').setValue('neues-secret')
    await wrapper.find('#sso-test-login').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(put).toHaveBeenCalledWith({
      oidc_enabled: true,
      oidc_discovery_url: 'https://idp.example/.well-known/openid-configuration',
      oidc_client_id: 'client-2',
      oidc_client_secret: 'neues-secret',
      oidc_scopes: 'openid',
      oidc_attribute_email: 'mail',
      oidc_attribute_name: 'anzeigename',
      test_login_enabled: true,
      saml_enabled: false,
      saml_idp_metadata_url: 'https://idp.example/saml/metadata',
      saml_idp_metadata_xml: '',
      saml_attribute_email: 'email',
      saml_attribute_name: 'name',
      saml_allow_unspecified_nameid: false,
    })
    expect(global.fetch).toHaveBeenCalledWith(SSO_URL, expect.objectContaining({
      method: 'PUT',
      headers: expect.objectContaining({ 'X-Auth-Token': 'tok-123' }),
    }))
    expect(wrapper.find('#sso-client-secret').element.value).toBe('')
    expect(wrapper.find('#sso-client-id').element.value).toBe('client-2')
    expect(wrapper.find('#sso-test-login').element.checked).toBe(true)
    expect(wrapper.find('.alert-success').text()).toContain('SSO-Konfiguration gespeichert.')

    await wrapper.find('.alert-close').trigger('click')
    expect(wrapper.find('.alert-success').exists()).toBe(false)
  })

  it('zeigt die Fehlermeldung des Backends, wenn das Speichern scheitert', async () => {
    const put = async () => jsonResponse({ error: 'OIDC enabled erfordert Discovery URL, Client ID und Secret' }, 400)
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ put }) })

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const alert = wrapper.find('.alert-error')
    expect(alert.text()).toContain('Speichern fehlgeschlagen: OIDC enabled erfordert Discovery URL, Client ID und Secret')
    expect(wrapper.find('form button[type="submit"]').attributes('disabled')).toBeUndefined()
  })

  it('zeigt den Speicherzustand am Button, solange der PUT läuft', async () => {
    let resolvePut
    const put = () => new Promise((resolve) => { resolvePut = resolve })
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ put }) })

    await wrapper.find('form').trigger('submit')
    await flushPromises()
    const button = wrapper.find('form button[type="submit"]')
    expect(button.text()).toBe('Speichern…')
    expect(button.attributes('disabled')).toBeDefined()

    resolvePut(jsonResponse(ssoFixture()))
    await flushPromises()
    expect(wrapper.find('form button[type="submit"]').text()).toBe('Speichern')
  })

  it('zeigt ForbiddenNotice, wenn das Speichern mit 403 abgelehnt wird', async () => {
    const put = async () => jsonResponse({}, 403)
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ put }) })

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Keine Berechtigung für diesen Bereich')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('löscht die Config nach Bestätigung und lädt den Default neu', async () => {
    const confirm = stubConfirm(true)
    let deleted = false
    const get = () => jsonResponse(deleted ? ssoFixture({ oidc_enabled: false, oidc_client_id: '', oidc_client_secret_set: false }) : ssoFixture())
    const del = vi.fn(async () => { deleted = true; return jsonResponse({ message: 'SSO-Config gelöscht' }) })
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get, del }) })

    await wrapper.find('.sso-delete').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith('SSO-Konfiguration wirklich löschen?')
    expect(del).toHaveBeenCalledTimes(1)
    expect(wrapper.find('#sso-client-id').element.value).toBe('')
    expect(wrapper.find('#sso-enabled').element.checked).toBe(false)
    expect(wrapper.find('.alert-success').text()).toContain('SSO-Konfiguration gelöscht.')
  })

  it('löscht nichts, wenn die Bestätigung abgebrochen wird', async () => {
    stubConfirm(false)
    const del = vi.fn()
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ del }) })

    await wrapper.find('.sso-delete').trigger('click')
    await flushPromises()

    expect(del).not.toHaveBeenCalled()
  })

  it('löscht die Seite, bei der gefragt wurde, auch wenn die Auswahl während des Dialogs wechselt', async () => {
    let answer
    setConfirmHandler(() => new Promise((resolve) => { answer = resolve }))
    const calls = []
    const fetchImpl = vi.fn(async (url, options = {}) => {
      calls.push(`${options.method || 'GET'} ${url}`)
      return jsonResponse(ssoFixture())
    })
    const wrapper = await mountAdminSSO({ fetchImpl })

    await wrapper.find('.sso-delete').trigger('click')
    useTenant().setActiveTenant('8')
    answer(true)
    await flushPromises()

    expect(calls).toContain(`DELETE ${SSO_URL}`)
    expect(calls).not.toContain('DELETE /api/admin/pages/8/sso')
  })

  it('zeigt einen Fehler, wenn das Löschen scheitert', async () => {
    stubConfirm(true)
    const del = async () => jsonResponse({ error: 'Fehler beim Löschen' }, 500)
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ del }) })

    await wrapper.find('.sso-delete').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Löschen fehlgeschlagen: Fehler beim Löschen')
    expect(wrapper.find('#sso-client-id').element.value).toBe('client-1')
  })

  it('zeigt ForbiddenNotice, wenn das Löschen mit 403 abgelehnt wird', async () => {
    stubConfirm(true)
    const del = async () => jsonResponse({}, 403)
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ del }) })

    await wrapper.find('.sso-delete').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Keine Berechtigung für diesen Bereich')
  })

  it('zeigt den Ladezustand, solange die Config lädt', async () => {
    let resolveGet
    const get = () => new Promise((resolve) => { resolveGet = resolve })
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get }) })

    const status = wrapper.find('[role="status"]')
    expect(status.text()).toBe('Lädt…')
    expect(status.attributes('aria-busy')).toBe('true')
    expect(wrapper.find('form').exists()).toBe(false)

    resolveGet(jsonResponse(ssoFixture()))
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(true)
  })

  it('zeigt einen Ladefehler statt des Formulars, wenn GET scheitert', async () => {
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse({ error: 'kaputt' }, 500) }) })

    expect(wrapper.find('[role="alert"]').text()).toBe('SSO-Konfiguration konnte nicht geladen werden.')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('zeigt ForbiddenNotice statt des Formulars bei 403', async () => {
    const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse({}, 403) }) })

    expect(wrapper.text()).toContain('Keine Berechtigung für diesen Bereich')
    expect(wrapper.text()).not.toContain('SSO-Konfiguration konnte nicht geladen werden.')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('zeigt einen Hinweis und lädt nichts, solange keine Statusseite gewählt ist', async () => {
    const fetchImpl = vi.fn()
    const wrapper = await mountAdminSSO({ fetchImpl, tenantId: null })

    expect(fetchImpl).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Bitte zuerst eine Statusseite auswählen.')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('lädt neu, wenn die aktive Statusseite wechselt, und setzt alte Fehler zurück', async () => {
    const fetchImpl = vi.fn(async (url) => {
      if (url === '/api/admin/pages/8/sso') return jsonResponse(ssoFixture({ status_page_id: 8, oidc_client_id: 'client-8' }))
      return jsonResponse({ error: 'kaputt' }, 500)
    })
    const wrapper = await mountAdminSSO({ fetchImpl })
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)

    useTenant().setActiveTenant('8')
    await flushPromises()

    expect(fetchImpl).toHaveBeenLastCalledWith('/api/admin/pages/8/sso', expect.anything())
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('#sso-client-id').element.value).toBe('client-8')
  })

  it('verknüpft alle Labels mit ihrem Feld', async () => {
    const wrapper = await mountAdminSSO()

    const labels = wrapper.findAll('label')
    for (const label of labels) {
      if (label.find('input,select,textarea').exists()) continue
      const id = label.attributes('for')
      expect(id, `Label „${label.text()}“ ohne for`).toBeTruthy()
      expect(wrapper.find(`#${id}`).exists(), `Feld #${id}`).toBe(true)
    }
    expect(wrapper.findAll('label[for]').length).toBe(18)
  })

  // Testanmeldung (test_login_enabled) aus der SSO-Seite heraus.
  describe('Testanmeldung', () => {
    it('zeigt den Knopf nur mit erlaubter Testanmeldung', async () => {
      const wrapper = await mountAdminSSO()
      expect(wrapper.find('.sso-test').exists()).toBe(false)

      await wrapper.find('#sso-test-login').setValue(true)

      expect(wrapper.find('.sso-test').exists()).toBe(true)
      expect(wrapper.text()).toContain('Nutzt die gespeicherte Konfiguration.')
    })

    it('startet die Testanmeldung und leitet zum Anbieter weiter', async () => {
      const test = vi.fn(async () => jsonResponse({ url: 'https://idp.example/authorize?state=s' }))
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ test_login_enabled: true })), test }) })

      await wrapper.find('.sso-test').trigger('click')
      await flushPromises()

      expect(test).toHaveBeenCalledTimes(1)
      expect(global.fetch).toHaveBeenCalledWith(`${SSO_URL}/test`, expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({ 'X-Auth-Token': 'tok-123' }),
      }))
      expect(window.location.assign).toHaveBeenCalledWith('https://idp.example/authorize?state=s')
    })

    it.each(['javascript:alert(1)', 'data:text/html,x', 'http://idp.example/authorize'])(
      'folgt keiner unsicheren Authorize-URL der Testanmeldung (%s)',
      async (url) => {
        vi.stubEnv('DEV', false)
        const test = vi.fn(async () => jsonResponse({ url }))
        const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ test_login_enabled: true })), test }) })

        await wrapper.find('.sso-test').trigger('click')
        await flushPromises()

        expect(test).toHaveBeenCalledTimes(1)
        expect(window.location.assign).not.toHaveBeenCalled()
        expect(wrapper.find('.alert-error').text()).toContain('unsichere Adresse')
      }
    )

    it('meldet, wenn die Testanmeldung nicht startet', async () => {
      const test = async () => jsonResponse({ error: 'Testanmeldung ist für diese Seite abgeschaltet' }, 400)
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ test_login_enabled: true })), test }) })

      await wrapper.find('.sso-test').trigger('click')
      await flushPromises()

      expect(wrapper.find('.alert-error').text()).toContain('Testanmeldung konnte nicht gestartet werden: Testanmeldung ist für diese Seite abgeschaltet')
      expect(window.location.assign).not.toHaveBeenCalled()
    })

    it('zeigt ForbiddenNotice, wenn der Start mit 403 abgelehnt wird', async () => {
      const test = async () => jsonResponse({}, 403)
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ test_login_enabled: true })), test }) })

      await wrapper.find('.sso-test').trigger('click')
      await flushPromises()

      expect(wrapper.text()).toContain('Keine Berechtigung für diesen Bereich')
    })

    it('zeigt den Erfolg aus ?oidc_test=ok', async () => {
      const wrapper = await mountAdminSSO({ search: '?oidc_test=ok' })

      const result = wrapper.find('[data-test="sso-test-result"]')
      expect(result.classes()).toContain('alert-success')
      expect(result.text()).toBe('Testanmeldung erfolgreich: Anbieter, Client-Daten und Redirect-URL passen.')
    })

    it('nennt fehlende Claims mit den konfigurierten Namen', async () => {
      const wrapper = await mountAdminSSO({
        fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ oidc_attribute_email: 'mail' })) }),
        search: '?oidc_test=ok&missing=email,name,quatsch',
      })

      const result = wrapper.find('[data-test="sso-test-result"]')
      expect(result.classes()).toContain('alert-warning')
      expect(result.text()).toContain('Im ID-Token fehlten diese Claims: mail, name')
      expect(result.text()).not.toContain('quatsch')
    })

    it('übersetzt den Fehlercode aus ?oidc_test=', async () => {
      const wrapper = await mountAdminSSO({ search: '?oidc_test=token_exchange' })

      const result = wrapper.find('[data-test="sso-test-result"]')
      expect(result.classes()).toContain('alert-error')
      expect(result.text()).toBe('Testanmeldung fehlgeschlagen: Der Anbieter hat die Anmeldung nicht bestätigt. Bitte erneut versuchen oder den Admin der Seite informieren.')
    })

    it('zeigt für unbekannte Codes die allgemeine Meldung', async () => {
      const wrapper = await mountAdminSSO({ search: '?oidc_test=%3Cb%3E' })

      expect(wrapper.find('[data-test="sso-test-result"]').text()).toBe('Testanmeldung fehlgeschlagen: Die SSO-Anmeldung ist fehlgeschlagen.')
    })

    it('blendet das Ergebnis per Schließen-Knopf aus', async () => {
      const wrapper = await mountAdminSSO({ search: '?oidc_test=ok' })

      await wrapper.find('[data-test="sso-test-result"] .alert-close').trigger('click')

      expect(wrapper.find('[data-test="sso-test-result"]').exists()).toBe(false)
    })
  })

  // SAML-Abschnitt der SSO-Seite.
  describe('SAML', () => {
    it('füllt die SAML-Felder und zeigt die Werte für den IdP', async () => {
      const wrapper = await mountAdminSSO()

      expect(wrapper.text()).toContain('SAML 2.0')
      expect(wrapper.find('#sso-saml-enabled').element.checked).toBe(false)
      expect(wrapper.find('#sso-saml-metadata-url').element.value).toBe('https://idp.example/saml/metadata')
      expect(wrapper.find('#sso-saml-metadata-xml').element.value).toBe('')
      expect(wrapper.find('#sso-saml-attribute-email').element.value).toBe('email')
      expect(wrapper.find('#sso-saml-attribute-name').element.value).toBe('name')
      expect(wrapper.find('#sso-saml-entity-id').element.value).toBe('http://localhost:8080/api/auth/saml/7/metadata')
      expect(wrapper.find('#sso-saml-acs-url').element.value).toBe('http://localhost:8080/api/auth/saml/7/acs')
      expect(wrapper.find('#sso-saml-certificate').element.value).toContain('BEGIN CERTIFICATE')
      const link = wrapper.find('a.sso-saml-metadata-link')
      expect(link.attributes('href')).toBe('http://localhost:8080/api/auth/saml/7/metadata')
      expect(link.attributes('target')).toBe('_blank')
      expect(link.attributes('rel')).toBe('noopener noreferrer')
    })

    it('verweist vor dem ersten Speichern auf später verfügbare Metadaten', async () => {
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ saml_sp_certificate: '' })) }) })

      expect(wrapper.find('a.sso-saml-metadata-link').exists()).toBe(false)
      expect(wrapper.find('#sso-saml-certificate').exists()).toBe(false)
      expect(wrapper.text()).toContain('Die SP-Metadaten stehen nach dem ersten Speichern bereit.')
    })

    it('verlangt bei aktivem SAML die Metadaten-URL, solange kein XML eingefügt ist', async () => {
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ saml_idp_metadata_url: '' })) }) })
      expect(wrapper.find('#sso-saml-metadata-url').attributes('required')).toBeUndefined()

      await wrapper.find('#sso-saml-enabled').setValue(true)
      expect(wrapper.find('#sso-saml-metadata-url').attributes('required')).toBeDefined()

      await wrapper.find('#sso-saml-metadata-xml').setValue('<EntityDescriptor/>')
      expect(wrapper.find('#sso-saml-metadata-url').attributes('required')).toBeUndefined()
    })

    it('speichert die SAML-Felder per PUT und übernimmt das neue Zertifikat', async () => {
      const put = vi.fn(async (body) => jsonResponse(ssoFixture({ ...body, saml_sp_certificate: 'NEU' })))
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ put, get: jsonResponse(ssoFixture({ saml_sp_certificate: '' })) }) })

      await wrapper.find('#sso-saml-enabled').setValue(true)
      await wrapper.find('#sso-saml-metadata-url').setValue('')
      await wrapper.find('#sso-saml-metadata-xml').setValue('<EntityDescriptor/>')
      await wrapper.find('#sso-saml-attribute-email').setValue('mail')
      await wrapper.find('#sso-saml-attribute-name').setValue('displayName')
      await wrapper.find('form').trigger('submit')
      await flushPromises()

      expect(put).toHaveBeenCalledWith(expect.objectContaining({
        saml_enabled: true,
        saml_idp_metadata_url: '',
        saml_idp_metadata_xml: '<EntityDescriptor/>',
        saml_attribute_email: 'mail',
        saml_attribute_name: 'displayName',
      }))
      expect(put.mock.calls[0][0]).not.toHaveProperty('saml_sp_certificate')
      expect(wrapper.find('#sso-saml-certificate').element.value).toBe('NEU')
    })

    // unspecified nur nach ausdrücklicher Freigabe,
    // mit Warnhinweis.
    it('erlaubt unspecified-NameIDs nur per Schalter und warnt dabei', async () => {
      const put = vi.fn(async (body) => jsonResponse(ssoFixture(body)))
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ put }) })

      const box = wrapper.find('#sso-saml-allow-unspecified')
      expect(box.element.checked).toBe(false)
      expect(wrapper.find('[data-test="saml-unspecified-warning"]').exists()).toBe(false)

      await box.setValue(true)
      expect(wrapper.find('[data-test="saml-unspecified-warning"]').text()).toContain('unspecified')
      await wrapper.find('form').trigger('submit')
      await flushPromises()

      expect(put).toHaveBeenCalledWith(expect.objectContaining({ saml_allow_unspecified_nameid: true }))
    })

    // Ein Wechsel von Issuer oder IdP-Metadaten löscht
    // beim Speichern die Verknüpfungen; die Seite warnt vorher.
    it('warnt vor dem Speichern, wenn Issuer oder IdP-Metadaten wechseln', async () => {
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ saml_idp_metadata_url: '', saml_idp_metadata_xml: '<EntityDescriptor/>' })) }) })
      const warning = () => wrapper.find('[data-test="sso-identity-reset-warning"]')
      expect(warning().exists()).toBe(false)

      // Gleicher Issuer, nur als Discovery-URL ohne .well-known bzw. mit Leerzeichen: kein Wechsel.
      await wrapper.find('#sso-discovery-url').setValue(' https://idp.example ')
      await wrapper.find('#sso-saml-metadata-xml').setValue('<EntityDescriptor/>\n')
      expect(warning().exists()).toBe(false)

      await wrapper.find('#sso-discovery-url').setValue('https://anderer-idp.example')
      expect(warning().text()).toContain('Verknüpfungen')
      await wrapper.find('#sso-discovery-url').setValue('https://idp.example/.well-known/openid-configuration')
      expect(warning().exists()).toBe(false)

      await wrapper.find('#sso-saml-metadata-xml').setValue('<EntityDescriptor entityID="neu"/>')
      expect(warning().exists()).toBe(true)
      await wrapper.find('#sso-saml-metadata-xml').setValue('')
      await wrapper.find('#sso-saml-metadata-url').setValue('https://idp.example/saml/metadata')
      expect(warning().exists()).toBe(true)
    })

    it('warnt bei erstmaliger Einrichtung nicht', async () => {
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ oidc_discovery_url: '', saml_idp_metadata_url: '' })) }) })

      await wrapper.find('#sso-discovery-url').setValue('https://idp.example')
      await wrapper.find('#sso-saml-metadata-url').setValue('https://idp.example/saml/metadata')

      expect(wrapper.find('[data-test="sso-identity-reset-warning"]').exists()).toBe(false)
    })

    it('startet die SAML-Testanmeldung und leitet zum IdP weiter', async () => {
      const samlTest = vi.fn(async () => jsonResponse({ url: 'https://idp.example/sso?SAMLRequest=x' }))
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ test_login_enabled: true })), samlTest }) })

      await wrapper.find('.sso-saml-test').trigger('click')
      await flushPromises()

      expect(samlTest).toHaveBeenCalledTimes(1)
      expect(global.fetch).toHaveBeenCalledWith(`${SSO_URL}/saml-test`, expect.objectContaining({ method: 'POST' }))
      expect(window.location.assign).toHaveBeenCalledWith('https://idp.example/sso?SAMLRequest=x')
    })

    it('meldet, wenn die SAML-Testanmeldung nicht startet', async () => {
      const samlTest = async () => jsonResponse({ error: 'Für die Testanmeldung fehlen die IdP-Metadaten' }, 400)
      const wrapper = await mountAdminSSO({ fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ test_login_enabled: true })), samlTest }) })

      await wrapper.find('.sso-saml-test').trigger('click')
      await flushPromises()

      expect(wrapper.find('.alert-error').text()).toContain('Testanmeldung konnte nicht gestartet werden: Für die Testanmeldung fehlen die IdP-Metadaten')
    })

    it('zeigt den SAML-Erfolg aus ?oidc_test=ok&sso=saml', async () => {
      const wrapper = await mountAdminSSO({ search: '?oidc_test=ok&sso=saml' })

      const result = wrapper.find('[data-test="sso-test-result"]')
      expect(result.classes()).toContain('alert-success')
      expect(result.text()).toBe('SAML-Testanmeldung erfolgreich: IdP-Metadaten, Signatur und ACS-URL passen.')
    })

    it('nennt fehlende SAML-Attribute mit den konfigurierten Namen', async () => {
      const wrapper = await mountAdminSSO({
        fetchImpl: makeFetch({ get: jsonResponse(ssoFixture({ saml_attribute_email: 'mail', saml_attribute_name: '' })) }),
        search: '?oidc_test=ok&sso=saml&missing=email,name',
      })

      const result = wrapper.find('[data-test="sso-test-result"]')
      expect(result.classes()).toContain('alert-warning')
      expect(result.text()).toContain('In der Assertion fehlten diese Attribute: mail, name')
    })

    it('übersetzt SAML-Fehlercodes', async () => {
      const wrapper = await mountAdminSSO({ search: '?oidc_test=saml_response_invalid' })

      expect(wrapper.find('[data-test="sso-test-result"]').text()).toBe('Testanmeldung fehlgeschlagen: Die Antwort des Anmeldedienstes war ungültig oder abgelaufen. Bitte erneut versuchen.')
    })
  })

  // Richtung C: Brotkrumen mit Seitenname, je Verfahren eine Karte im
  // gemeinsamen Formular (OIDC, SAML, Testanmeldung).
  it('zeigt Brotkrumen mit Seitenname und je Verfahren eine Karte im selben Formular', async () => {
    useTenant().setMyPages([{ ID: 7, Name: 'Acme' }])
    const wrapper = await mountAdminSSO()

    const crumbs = wrapper.findAll('.page-head__crumbs li').map(li => li.text())
    expect(crumbs).toEqual(['Acme', 'SSO Konfiguration'])
    const form = wrapper.find('form')
    expect(form.findAll('section.comp-card h3').map(h => h.text())).toEqual(['OpenID Connect (OIDC)', 'SAML 2.0', 'Testanmeldung'])
    expect(form.findAll('h3.sso-section')).toHaveLength(0)
    useTenant().setMyPages([])
  })

  // Rechte der Ressource sso je Aktion.
  it('sperrt das Formular ohne sso:update und blendet Speichern aus', async () => {
    useTenant().setActiveTenant(7)
    setAuthAsPageRole('tok-1', { is_global_admin: false, permissions: 'sso:read' })
    const wrapper = await mountAdminSSO()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.form-lock-hint').text()).toContain('Nur Lesezugriff')
    expect(wrapper.find('button[type="submit"]').exists()).toBe(false)
    expect(wrapper.find('.sso-delete').exists()).toBe(false)
  })

  it('zeigt Löschen nur mit sso:delete und Speichern nur mit sso:update', async () => {
    useTenant().setActiveTenant(7)
    setAuthAsPageRole('tok-1', { is_global_admin: false, permissions: 'sso:delete' })
    let wrapper = await mountAdminSSO()
    expect(wrapper.find('.sso-delete').exists()).toBe(true)
    expect(wrapper.find('button[type="submit"]').exists()).toBe(false)
    wrapper.unmount()
    useTenant().setActiveTenant(7)
    setAuthAsPageRole('tok-1', { is_global_admin: false, permissions: 'sso:update' })
    wrapper = await mountAdminSSO()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('button[type="submit"]').exists()).toBe(true)
    expect(wrapper.find('.sso-delete').exists()).toBe(false)
  })
})
