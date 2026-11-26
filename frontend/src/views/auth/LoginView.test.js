import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useAuthConfig, resetAuthConfig } from '../../composables/useAuthConfig'
import LoginView from './LoginView.vue'
import loginSource from './LoginView.vue?raw'
import { axe } from '../../test-support/axe'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      { path: '/login', component: LoginView },
      { path: '/status/:slug', component: { template: '<div/>' } },
      { path: '/admin', component: { template: '<div/>' } },
      { path: '/admin/incidents', component: { template: '<div/>' } },
    ],
  })
}

// returnTo is attacker input: every one of these must end on the landing page "/".
const ABUSE = [
  ['absolute URL', 'https://angreifer.example/phish'],
  ['protocol-relative', '//angreifer.example'],
  ['slash + backslash', '/\\angreifer.example'],
  ['javascript:', 'javascript:alert(1)'],
  ['encoded //', '%2F%2Fangreifer.example'],
  ['slash + encoded slash', '/%2F/angreifer.example'],
  ['newline', '/admin%0A//x'],
  ['tab', '/admin%09'],
  ['leading space', '%20/admin'],
  ['overlength', '/status/' + 'a'.repeat(3000)],
  ['unknown route', '/gibt/es/nicht'],
]

async function mountLogin(search = '') {
  vi.stubGlobal('location', { ...window.location, search })
  const router = makeRouter()
  router.push('/login')
  await router.isReady()

  const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })
  return { wrapper, router }
}

// The auth config is cached module-wide; once seeded the view does not load it,
// and global.fetch only sees the login request.
function seedAuthConfig(config = {}) {
  useAuthConfig().authConfig.value = { passwordLogin: true, providers: [], ...config }
}

describe('LoginView', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    seedAuthConfig()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
    useAuth().logout()
    resetAuthConfig()
  })

  // The password form only appears when the backend explicitly allows it.
  describe('password switch', () => {
    it('hides the password form when password_login is off', async () => {
      seedAuthConfig({ passwordLogin: false, providers: [{ id: 'gitlab', name: 'GitLab' }] })
      const { wrapper } = await mountLogin()

      expect(wrapper.find('form.login-form').exists()).toBe(false)
      expect(wrapper.find('.entry-divider').exists()).toBe(false)
      expect(wrapper.find('.provider-login-btn').exists()).toBe(true)
    })

    it('hides the password form while the config is unknown (loading or failed)', async () => {
      resetAuthConfig()
      vi.spyOn(console, 'error').mockImplementation(() => {})
      let fail
      global.fetch = vi.fn().mockReturnValue(new Promise((resolve) => { fail = resolve }))
      const { wrapper } = await mountLogin()

      expect(wrapper.find('form.login-form').exists()).toBe(false)
      fail(jsonResponse({}, 500))
      await flushPromises()
      expect(wrapper.find('form.login-form').exists()).toBe(false)
      expect(wrapper.find('[data-test="no-login-method"]').exists()).toBe(false)
    })

    it('shows the divider only when a sign-in button sits above the password form', async () => {
      let { wrapper } = await mountLogin()
      expect(wrapper.find('form.login-form').exists()).toBe(true)
      expect(wrapper.find('.entry-divider').exists()).toBe(false)
      wrapper.unmount()

      seedAuthConfig({ providers: [{ id: 'gitlab', name: 'GitLab' }] })
      ;({ wrapper } = await mountLogin())
      expect(wrapper.find('.entry-divider').text()).toBe('oder mit E-Mail & Passwort')
    })

    it('shows the divider for page SSO above the password form', async () => {
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ oidc_enabled: true, page_id: 5 }))
      const { wrapper } = await mountLogin('?page=acme')
      await flushPromises()

      expect(wrapper.find('.entry-divider').exists()).toBe(true)
    })

    it.each([
      ['de', 'Keine Anmeldemethode konfiguriert. Wende dich an den Betreiber.'],
      ['en', 'No sign-in method is configured. Please contact the operator.'],
    ])('shows a notice when no sign-in method is configured (%s)', async (locale, expected) => {
      i18n.global.locale.value = locale
      seedAuthConfig({ passwordLogin: false, providers: [] })
      const { wrapper } = await mountLogin()

      expect(wrapper.find('[data-test="no-login-method"]').text()).toBe(expected)
      expect(wrapper.find('form.login-form').exists()).toBe(false)
    })

    it('waits for the page SSO answer before showing the no-method notice', async () => {
      seedAuthConfig({ passwordLogin: false, providers: [] })
      let answer
      global.fetch = vi.fn().mockReturnValue(new Promise((resolve) => { answer = resolve }))
      const { wrapper } = await mountLogin('?page=acme')
      await flushPromises()
      expect(wrapper.find('[data-test="no-login-method"]').exists()).toBe(false)

      answer(jsonResponse({ oidc_enabled: false, page_id: 5 }))
      await flushPromises()
      expect(wrapper.find('[data-test="no-login-method"]').exists()).toBe(true)
    })

    it('shows no notice when page SSO is offered', async () => {
      seedAuthConfig({ passwordLogin: false, providers: [] })
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ oidc_enabled: true, page_id: 5 }))
      const { wrapper } = await mountLogin('?page=acme')
      await flushPromises()

      expect(wrapper.find('a.oidc-login-btn').exists()).toBe(true)
      expect(wrapper.find('[data-test="no-login-method"]').exists()).toBe(false)
    })

    // The backend refuses with 403 password_login_disabled when the switch flipped meanwhile.
    it.each([
      ['de', 'Die Anmeldung mit E-Mail und Passwort ist abgeschaltet.'],
      ['en', 'Signing in with email and password is disabled.'],
    ])('translates 403 password_login_disabled (%s)', async (locale, expected) => {
      i18n.global.locale.value = locale
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'password login disabled', code: 'password_login_disabled' }, 403))
      const { wrapper } = await mountLogin()

      await wrapper.find('form.login-form').trigger('submit.prevent')
      await flushPromises()

      expect(wrapper.find('.login-alert.error').text()).toBe(expected)
      expect(useAuth().isLoggedIn.value).toBe(false)
    })
  })

  // Global OIDC providers come from GET /api/public/auth-config.
  describe('global providers', () => {
    const GITLAB = { id: 'gitlab', name: 'GitLab' }

    it('shows one button per active provider with its name', async () => {
      seedAuthConfig({ providers: [GITLAB, { id: 'corp', name: 'Corp SSO' }] })
      const { wrapper } = await mountLogin()

      const buttons = wrapper.findAll('.provider-login-btn')
      expect(buttons.map((b) => b.text())).toEqual(['Mit GitLab anmelden', 'Mit Corp SSO anmelden'])
    })

    it('starts the login by browser navigation to the provider endpoint', async () => {
      seedAuthConfig({ providers: [GITLAB] })
      const assign = vi.fn()
      vi.stubGlobal('location', { ...window.location, search: '', assign })
      const router = makeRouter()
      router.push('/login')
      await router.isReady()
      const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })

      await wrapper.find('.provider-login-btn').trigger('click')

      expect(assign).toHaveBeenCalledTimes(1)
      expect(assign).toHaveBeenCalledWith('/api/auth/provider/gitlab/login?return_to=%2F')
    })

    it('uses VITE_API_URL and encodes the provider id', async () => {
      vi.stubEnv('VITE_API_URL', 'https://api.example.org')
      seedAuthConfig({ providers: [{ id: 'a/b?c', name: 'Odd' }] })
      const assign = vi.fn()
      vi.stubGlobal('location', { ...window.location, search: '', assign })
      const router = makeRouter()
      router.push('/login')
      await router.isReady()
      const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })

      await wrapper.find('.provider-login-btn').trigger('click')

      expect(assign).toHaveBeenCalledWith('https://api.example.org/api/auth/provider/a%2Fb%3Fc/login?return_to=%2F')
    })

    it('passes the checked returnTo as return_to to the provider start', async () => {
      seedAuthConfig({ providers: [GITLAB] })
      const assign = vi.fn()
      vi.stubGlobal('location', { ...window.location, search: '?returnTo=/status/acme?lang=en&page=acme', assign })
      const router = makeRouter()
      router.push('/login')
      await router.isReady()
      const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })

      await wrapper.find('.provider-login-btn').trigger('click')

      expect(assign).toHaveBeenCalledWith('/api/auth/provider/gitlab/login?return_to=%2Fstatus%2Facme%3Flang%3Den')
    })

    it.each(ABUSE)('sends return_to=/ to the provider for a malicious returnTo (%s)', async (_, evil) => {
      seedAuthConfig({ providers: [GITLAB] })
      const assign = vi.fn()
      vi.stubGlobal('location', { ...window.location, search: '?returnTo=' + evil, assign })
      const router = makeRouter()
      router.push('/login')
      await router.isReady()
      const wrapper = mount(LoginView, { global: { plugins: [i18n, router] } })

      await wrapper.find('.provider-login-btn').trigger('click')

      expect(assign).toHaveBeenCalledWith('/api/auth/provider/gitlab/login?return_to=%2F')
    })

    // The name comes from the server and is only rendered as text, never as HTML.
    it('renders the provider name as text, not as HTML', async () => {
      seedAuthConfig({ providers: [{ id: 'x', name: '<img src=x onerror=alert(1)>' }] })
      const { wrapper } = await mountLogin()

      const button = wrapper.find('.provider-login-btn')
      expect(button.text()).toBe('Mit <img src=x onerror=alert(1)> anmelden')
      expect(button.find('img').exists()).toBe(false)
      expect(loginSource).not.toContain('v-html')
    })

    it('loads the config from the server once when it is not known yet', async () => {
      resetAuthConfig()
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ password_login: true, providers: [GITLAB] }))
      const { wrapper } = await mountLogin()
      await flushPromises()

      expect(global.fetch).toHaveBeenCalledTimes(1)
      expect(global.fetch).toHaveBeenCalledWith('/api/public/auth-config', expect.anything())
      expect(wrapper.find('.provider-login-btn').text()).toBe('Mit GitLab anmelden')
    })

    it('shows a notice on a load error and invents no buttons', async () => {
      resetAuthConfig()
      vi.spyOn(console, 'error').mockImplementation(() => {})
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'broken' }, 500))
      const { wrapper } = await mountLogin()
      await flushPromises()

      expect(wrapper.find('[data-test="auth-config-error"]').text())
        .toBe('Die Anmeldemethoden konnten nicht geladen werden. Bitte lade die Seite neu.')
      expect(wrapper.find('.provider-login-btn').exists()).toBe(false)
    })
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

  it.each([
    ['de', 'Dieser Anmeldeanbieter ist unbekannt oder nicht mehr aktiv.'],
    ['en', 'This sign-in provider is unknown or no longer active.'],
  ])('translates the callback error provider_unknown (%s)', async (locale, expected) => {
    i18n.global.locale.value = locale
    const { wrapper } = await mountLogin('?oidc_error=provider_unknown')

    expect(wrapper.find('.login-alert.error').text()).toBe(expected)
  })

  it('zeigt bei unbekanntem OIDC-Fehlercode eine allgemeine Meldung statt des Codes', async () => {
    const { wrapper } = await mountLogin('?oidc_error=%3Cb%3Ehallo')

    const alert = wrapper.find('.login-alert.error')
    expect(alert.text()).toBe('Die SSO-Anmeldung ist fehlgeschlagen.')
    expect(alert.text()).not.toContain('hallo')
  })

  it('shows the page SSO button for ?page= and passes the return target on', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ oidc_enabled: true, page_id: 5 }))
    const { wrapper } = await mountLogin('?page=acme&returnTo=/status/acme')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledWith('/api/public/pages/acme/sso', expect.anything())
    expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('/api/auth/oidc/login?page=5&return_to=%2Fstatus%2Facme')
  })

  it('offers the slug entry for ?page= when the lookup answers 404 (private page), with return target', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ code: 'page_not_found' }, 404))
    const { wrapper } = await mountLogin('?page=geheim')
    await flushPromises()

    expect(wrapper.find('a.oidc-login-btn').attributes('href')).toBe('/api/auth/sso/login?slug=geheim&return_to=%2F')
  })

  it('zeigt ohne ?page= keinen Knopf für das Seiten-SSO', async () => {
    global.fetch = vi.fn()
    const { wrapper } = await mountLogin()

    expect(wrapper.findComponent({ name: 'OidcLoginButton' }).exists()).toBe(false)
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('signs in with email/password and goes to the landing page "/" by default', async () => {
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
    // without id the user management does not recognise the own row.
    expect(useAuth().currentUser.value.id).toBe(4)
    // switched-off help tooltips apply right after the login.
    expect(useAuth().currentUser.value.help_tooltips).toBe(false)
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('goes back to the status page it came from after a password login', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))
    const { wrapper, router } = await mountLogin('?returnTo=' + encodeURIComponent('/status/acme?lang=en') + '&page=acme')
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))

    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(router.currentRoute.value.fullPath).toBe('/status/acme?lang=en')
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

  // Open redirect: a manipulated returnTo must never lead to a foreign address.
  it.each(ABUSE)('goes to "/" after the login for a malicious returnTo (%s)', async (_, boese) => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ token: 'jwt-1', email: 'a@b.de' }))
    const { wrapper, router } = await mountLogin('?returnTo=' + boese)
    const push = vi.spyOn(router, 'push')

    await wrapper.find('#login-email').setValue('a@b.de')
    await wrapper.find('#login-password').setValue('secret123')
    await wrapper.find('form.login-form').trigger('submit.prevent')
    await flushPromises()

    expect(push).toHaveBeenCalledTimes(1)
    expect(push).toHaveBeenCalledWith('/')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('never calls location.assign with the foreign address for a malicious returnTo', async () => {
    const assign = vi.fn()
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: () => Promise.reject(new TypeError('Failed to fetch dynamically imported module')) },
        { path: '/login', component: LoginView },
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
    expect(assign).toHaveBeenCalledWith('/')
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

  it('keeps the sign-in button disabled until the navigation after the login has finished', async () => {
    let resolveRoute
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
        { path: '/admin', component: () => new Promise((resolve) => { resolveRoute = resolve }) },
      ],
    })
    vi.stubGlobal('location', { ...window.location, search: '?returnTo=/admin' })
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

  it('shows no login error when the navigation after a successful login fails and reloads the target', async () => {
    const assign = vi.fn()
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
        { path: '/admin', component: () => Promise.reject(new TypeError('Failed to fetch dynamically imported module')) },
      ],
    })
    vi.stubGlobal('location', { ...window.location, search: '?returnTo=/admin', assign })
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

  // Fixed Google/GitHub buttons are gone, and so are their texts.
  it('shows no fixed Google/GitHub buttons', async () => {
    const { wrapper } = await mountLogin()

    expect(wrapper.text()).not.toMatch(/Google|GitHub/)
    expect(wrapper.find('.sso-btn').exists()).toBe(false)
    expect(i18n.global.te('auth.login.googleButton')).toBe(false)
    expect(i18n.global.te('auth.login.githubButton')).toBe(false)
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
