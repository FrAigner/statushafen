import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import LoginView from './LoginView.vue'
import { axe } from '../../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: LoginView },
      { path: '/admin', component: { template: '<div/>' } },
      { path: '/admin/incidents', component: { template: '<div/>' } },
    ],
  })
}

async function mountLogin(search = '') {
  vi.stubGlobal('location', { ...window.location, search })
  const router = makeRouter()
  router.push('/login')
  await router.isReady()

  const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })
  return { wrapper, router }
}

describe('LoginView', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    useAuth().logout()
  })

  // Branding: Der Login-Titel führt den echten Produktnamen
  // "Statushafen" statt des Vorlagen-Platzhalters "Status Enterprise CMS".
  it('zeigt "Statushafen" als Titel', async () => {
    const { wrapper } = await mountLogin()

    expect(wrapper.find('.login-title').text()).toBe('Statushafen')
    expect(wrapper.text()).not.toContain('Status Enterprise CMS')
  })

  it('zeigt keine Fehlermeldung ohne error-Query-Parameter', async () => {
    const { wrapper } = await mountLogin()

    expect(wrapper.find('.login-alert.error').exists()).toBe(false)
  })

  it('zeigt die SSO-Fehlermeldung bei ?error=sso_failed', async () => {
    const { wrapper } = await mountLogin('?error=sso_failed')

    expect(wrapper.find('.login-alert.error').exists()).toBe(true)
    expect(wrapper.find('.login-alert.error').attributes('role')).toBe('alert')
  })

  // Fehler aus dem OIDC-Callback kommen als fester Code.
  it('übersetzt den OIDC-Fehlercode aus ?oidc_error=', async () => {
    const { wrapper } = await mountLogin('?oidc_error=not_linked')

    expect(wrapper.find('.login-alert.error').text()).toContain('mit keinem Konto verknüpft')
  })

  it('zeigt bei unbekanntem OIDC-Fehlercode eine allgemeine Meldung statt des Codes', async () => {
    const { wrapper } = await mountLogin('?oidc_error=%3Cb%3Ehallo')

    const alert = wrapper.find('.login-alert.error')
    expect(alert.text()).toBe('Die SSO-Anmeldung ist fehlgeschlagen.')
    expect(alert.text()).not.toContain('hallo')
  })

  it('zeigt bei ?page= den Knopf für das Seiten-SSO', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ oidc_enabled: true, page_id: 5 }))
    const { wrapper } = await mountLogin('?page=acme')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledWith('/api/public/pages/acme/sso', expect.anything())
    expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('/api/auth/oidc/login?page=5')
  })

  it('bietet bei ?page= den Einstieg über den Slug an, wenn die Auskunft 404 liefert (private Seite)', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ code: 'page_not_found' }, 404))
    const { wrapper } = await mountLogin('?page=geheim')
    await flushPromises()

    expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('/api/auth/sso/login?slug=geheim')
  })

  it('zeigt ohne ?page= keinen Knopf für das Seiten-SSO', async () => {
    global.fetch = vi.fn()
    const { wrapper } = await mountLogin()

    expect(wrapper.findComponent({ name: 'OidcLoginButton' }).exists()).toBe(false)
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('loggt per E-Mail/Passwort ein und leitet standardmäßig zu /admin weiter (erster lesbarer Tab)', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({
      token: 'jwt-1', id: 4, email: 'a@b.de', role: 'Admin', is_global_admin: true, help_tooltips: false,
    }))
    const { wrapper, router } = await mountLogin()

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/login'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ email: 'a@b.de', password: 'secret123' }) })
    )
    expect(useAuth().isLoggedIn.value).toBe(true)
    // ohne id erkennt die Benutzerverwaltung die eigene Zeile nicht.
    expect(useAuth().currentUser.value.id).toBe(4)
    // ausgeschaltete Hilfe-Tooltips gelten nach dem Login sofort.
    expect(useAuth().currentUser.value.help_tooltips).toBe(false)
    expect(router.currentRoute.value.path).toBe('/admin')
  })

  it('respektiert einen returnTo-Query-Parameter nach erfolgreichem Login', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))
    const { wrapper, router } = await mountLogin('?returnTo=/admin/incidents')

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/admin/incidents')
  })

  // Open Redirect. Ein manipulierter returnTo darf nie zu einer fremden Adresse führen.
  it.each([
    'https://angreifer.example/phish',
    '//angreifer.example',
    '/\\angreifer.example',
    'javascript:alert(1)',
    '%2F%2Fangreifer.example',
    '/%2F/angreifer.example',
    '/admin%0A//x',
  ])('geht bei bösem returnTo %s nach dem Login auf /admin', async (boese) => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))
    const { wrapper, router } = await mountLogin('?returnTo=' + boese)
    const push = vi.spyOn(router, 'push')

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(push).toHaveBeenCalledTimes(1)
    expect(push).toHaveBeenCalledWith('/admin')
    expect(router.currentRoute.value.path).toBe('/admin')
  })

  it('ruft location.assign bei bösem returnTo nie mit der fremden Adresse auf', async () => {
    const assign = vi.fn()
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
        { path: '/admin', component: () => Promise.reject(new TypeError('Failed to fetch dynamically imported module')) },
      ],
    })
    vi.stubGlobal('location', { ...window.location, search: '?returnTo=https://angreifer.example/x', assign })
    router.push('/login')
    await router.isReady()
    const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(assign).toHaveBeenCalledTimes(1)
    expect(assign).toHaveBeenCalledWith('/admin')
  })

  it('zeigt eine Fehlermeldung bei ungültigen Zugangsdaten und bleibt eingeloggt aus', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'Ungültige Zugangsdaten' }, 401))
    const { wrapper } = await mountLogin()

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('wrong')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.login-alert.error').exists()).toBe(true)
    expect(useAuth().isLoggedIn.value).toBe(false)
  })

  // Falsches Passwort zeigte „Sitzung abgelaufen. Bitte neu einloggen.“,
  // weil useApi jede 401 als Sitzungsende behandelte.
  it.each([
    ['de', 'E-Mail oder Passwort ist falsch.'],
    ['en', 'Incorrect email or password.'],
  ])('meldet bei falschem Passwort falsche Zugangsdaten statt Sitzungsende (%s)', async (locale, expected) => {
    i18n.global.locale.value = locale
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'Invalid email or password' }, 401))
    const { wrapper } = await mountLogin()

    await wrapper.find('#login-email').setValue('admin@statushafen.local')
    await wrapper.find('#login-password').setValue('falsch')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    const alert = wrapper.find('.login-alert.error')
    expect(alert.text()).toBe(expected)
    expect(alert.text()).not.toMatch(/Sitzung abgelaufen|Session expired/)
    expect(alert.attributes('role')).toBe('alert')
  })

  // Ein noch gespeichertes, veraltetes Token darf den Login-Fehlversuch
  // nicht zum Sitzungsende machen und wird beim Login nicht mitgeschickt.
  it('meldet falsche Zugangsdaten auch bei noch gespeichertem Token', async () => {
    useAuth().token.value = 'jwt-alt'
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'Invalid email or password' }, 401))
    const { wrapper } = await mountLogin()

    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(global.fetch.mock.calls[0][1].headers['X-Auth-Token']).toBeUndefined()
    expect(wrapper.find('.login-alert.error').text()).toBe('E-Mail oder Passwort ist falsch.')
  })

  // Eine 401 von Caddys Basic Auth ist kein Passwortfehler der App.
  it('zeigt bei einer Basic-Auth-401 nicht „Passwort falsch“', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ...jsonResponse({}, 401),
      headers: new Headers({ 'WWW-Authenticate': 'Basic realm="restricted"' }),
    })
    const { wrapper } = await mountLogin()

    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.login-alert.error').text())
      .toBe('Login fehlgeschlagen: Zugang zum Server verweigert (vorgeschaltete Anmeldung).')
  })

  it('sperrt den Anmelden-Button während der Anfrage und sendet bei Doppel-Submit nur einen Request', async () => {
    let resolveFetch
    global.fetch = vi.fn().mockReturnValue(new Promise((resolve) => { resolveFetch = resolve }))
    const { wrapper } = await mountLogin()

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await wrapper.find('form.login-form').trigger('submit.prevent')

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(wrapper.find('button.btn-login').attributes('disabled')).toBeDefined()

    resolveFetch(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))
    await flushPromises()
  })

  it('hält den Anmelden-Button gesperrt, bis die Navigation nach dem Login abgeschlossen ist', async () => {
    let resolveRoute
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
        { path: '/admin', component: () => new Promise((resolve) => { resolveRoute = resolve }) },
      ],
    })
    vi.stubGlobal('location', { ...window.location, search: '' })
    router.push('/login')
    await router.isReady()
    const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('button.btn-login').attributes('disabled')).toBeDefined()
    await wrapper.find('form.login-form').trigger('submit.prevent')
    expect(global.fetch).toHaveBeenCalledTimes(1)

    resolveRoute({ template: '<div/>' })
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin')
  })

  it('zeigt bei fehlgeschlagener Navigation nach erfolgreichem Login keine Login-Fehlermeldung und lädt die Zielseite neu', async () => {
    const assign = vi.fn()
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
        { path: '/admin', component: () => Promise.reject(new TypeError('Failed to fetch dynamically imported module')) },
      ],
    })
    vi.stubGlobal('location', { ...window.location, search: '', assign })
    router.push('/login')
    await router.isReady()
    const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.login-alert.error').exists()).toBe(false)
    expect(useAuth().isLoggedIn.value).toBe(true)
    expect(assign).toHaveBeenCalledTimes(1)
    expect(assign).toHaveBeenCalledWith('/admin')
    expect(wrapper.find('button.btn-login').attributes('disabled')).toBeUndefined()
  })

  it('gibt den Anmelden-Button nach einem Fehler wieder frei', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'Ungültige Zugangsdaten' }, 401))
    const { wrapper } = await mountLogin()

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('wrong')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('button.btn-login').attributes('disabled')).toBeUndefined()
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()
    expect(global.fetch).toHaveBeenCalledTimes(2)
  })

  it('togglet die Passwort-Sichtbarkeit', async () => {
    const { wrapper } = await mountLogin()

    const input = wrapper.find('#login-password')
    expect(input.attributes('type')).toBe('password')

    await wrapper.find('.password-toggle').trigger('click')
    expect(input.attributes('type')).toBe('text')
  })

  it('leitet per SSO-Button-Klick zum Provider-Endpunkt weiter', async () => {
    vi.stubGlobal('location', { ...window.location, search: '', href: '' })
    const { wrapper } = await mountLogin()

    await wrapper.findAll('.sso-btn')[0].trigger('click')

    expect(window.location.href).toContain('/api/auth/google')
  })

  it('hat keine axe-Funde, auch mit Fehlermeldung', async () => {
    const { wrapper } = await mountLogin('?error=sso_failed')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Pflichtfelder tragen Stern (Klasse), aria-required und den Hinweis.
  it('kennzeichnet E-Mail und Passwort als Pflichtfelder', async () => {
    const { wrapper } = await mountLogin()
    for (const id of ['login-email', 'login-password']) {
      expect(wrapper.find(`label[for="${id}"]`).classes()).toContain('onyx-required-marker')
      expect(wrapper.find(`#${id}`).attributes('aria-required')).toBe('true')
    }
    expect(wrapper.find('[data-test="required-hint"]').text()).toBe('Felder mit * sind Pflichtfelder.')
  })
})
