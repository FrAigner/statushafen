import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createOnyx } from 'sit-onyx'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { useTenant } from '../composables/useTenant'
import { setAuthAsPageRole } from '../test-support/pageRole'
import { useCurrentPageSlug } from '../composables/useCurrentPageSlug'
import { useLandingChrome } from '../composables/useLandingChrome'
import PublicLayout from './PublicLayout.vue'
import layoutSource from './PublicLayout.vue?raw'
import { axe } from '../test-support/axe.js'

const Empty = { template: '<div />' }

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Empty },
      { path: '/login', component: Empty },
      { path: '/admin/components', component: Empty },
      { path: '/docs', component: Empty },
    ],
  })
}

async function mountLayout() {
  const router = makeRouter()
  router.push('/')
  await router.isReady()

  const wrapper = mount(PublicLayout, {
    global: { plugins: [i18n, router, createOnyx()] },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('PublicLayout', () => {
  beforeEach(() => {
    // Das Layout lädt /api/me/pages selbst; ohne Stub ginge die Anfrage ins Leere.
    globalThis.fetch = vi.fn(async () => ({ ok: true, status: 200, json: async () => [] }))
    useAuth().logout()
    useLandingChrome().isLanding.value = false
    useLandingChrome().ordersEnabled.value = true
    localStorage.clear()
    // Tests assertieren auf den deutschen Wortlaut übersetzter Menüpunkte;
    // ohne gespeicherte Präferenz würde detectInitialLocale() in der Testumgebung
    // (happy-dom navigator.language === 'en-US') sonst auf 'en' fallen.
    i18n.global.locale.value = 'de'
  })

  // Die Token-Ebene „public“ (Richtung A, tokens.css) hängt an dieser Klasse.
  it('trägt die Token-Ebene theme-public am Wurzelelement, nicht theme-admin', async () => {
    const { wrapper } = await mountLayout()
    expect(wrapper.classes()).toContain('theme-public')
    expect(wrapper.classes()).not.toContain('theme-admin')
  })

  // Regression 2026-10-29: Die Sprachauswahl war 28 statt 44 px hoch.
  // LanguageSwitcher hat zwei Wurzelelemente, das scoped-Attribut des Layouts
  // landet nicht am <select>, eine normale scoped-Regel greift dort nie.
  // happy-dom rechnet kein Layout, deshalb: Klasse am <select> und die
  // :deep()-Regel mit der gemeinsamen Höhe im Quelltext.
  it('Sprachauswahl hat die Höhe der übrigen Kopfzeilen-Elemente', async () => {
    const { wrapper } = await mountLayout()

    const select = wrapper.find('header select.language-switcher')
    expect(select.classes()).toContain('header-control')
    const rule = layoutSource.match(/\.app-header :deep\(\.header-control\.language-switcher\) \{([^}]*)\}/)?.[1] ?? ''
    expect(rule).toMatch(/height: var\(--header-control-size\)/)
    expect(layoutSource).not.toMatch(/^\.header-control\.language-switcher \{/m)
  })

  it('Logo und Name führen zur Startseite', async () => {
    const { wrapper, router } = await mountLayout()
    await router.push('/login')

    const logo = wrapper.find('a.logo')
    expect(logo.exists()).toBe(true)
    expect(logo.attributes('href')).toBe('/')

    await logo.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/')
  })

  // Branding: Der Kopf zeigt den echten Produktnamen "Statushafen"
  // statt des Vorlagen-Platzhalters "Onyx Status Enterprise".
  it('zeigt "Statushafen" als Branding im Logo', async () => {
    const { wrapper } = await mountLayout()

    const logo = wrapper.find('.logo')
    expect(logo.text()).toBe('Statushafen')
    expect(wrapper.text()).not.toContain('Onyx Status Enterprise')
  })

  // Das Leuchtturm-Icon ist ein Inline-SVG mit currentColor (kein
  // dunkelblaues PNG mehr, das im Dunkelmodus gefiltert werden musste).
  it('zeigt das Leuchtturm-Logo als SVG neben der Wortmarke im Logo', async () => {
    const { wrapper } = await mountLayout()

    const logo = wrapper.find('.logo')
    expect(logo.find('svg.logo-mark').exists()).toBe(true)
    expect(logo.find('img').exists()).toBe(false)
    expect(layoutSource).not.toContain('/logo.png')
    expect(logo.text()).toContain('Statushafen')
  })

  it('zeigt den Admin-Login-Button, wenn niemand eingeloggt ist', async () => {
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.login-button').exists()).toBe(true)
    expect(wrapper.find('.user-menu-trigger').exists()).toBe(false)
  })

  it('navigiert beim Klick auf den Admin-Login-Button zu /login', async () => {
    const { wrapper, router } = await mountLayout()

    await wrapper.find('.login-button').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/login')
  })

  // Der Login-Knopf war früher ein OnyxButton und wich optisch von
  // Theme-Toggle und Sprachauswahl ab (andere Höhe/Radius, Serifen-Schrift).
  // Jetzt ist es ein nativer Button mit der gemeinsamen header-control-Optik.
  it('rendert den Login-Knopf ausgeloggt als nativen header-control-Button, nicht als OnyxButton', async () => {
    const { wrapper } = await mountLayout()

    const loginButton = wrapper.find('.login-button.header-control')
    expect(loginButton.exists()).toBe(true)
    expect(loginButton.element.tagName).toBe('BUTTON')
    expect(loginButton.classes()).toContain('header-control--text')
    expect(loginButton.classes()).not.toContain('onyx-button')
    expect(loginButton.text()).toBe('Anmelden')
  })

  // Regressions-Guard: Der Login-Knopf teilt dieselbe Basisklasse header-control
  // wie Theme-Toggle und Sprachauswahl, damit alle drei Controls einheitlich aussehen.
  it('gibt dem Login-Knopf dieselbe header-control-Basisklasse wie Theme-Toggle und Sprachauswahl', async () => {
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.login-button').classes()).toContain('header-control')
    expect(wrapper.find('.theme-toggle').classes()).toContain('header-control')
    expect(wrapper.find('.language-switcher').classes()).toContain('header-control')
  })

  // Die Login-Seite zeigt den SSO-Knopf der gerade angezeigten Statusseite.
  it('gibt die angezeigte Statusseite als ?page= an die Login-Seite weiter', async () => {
    useCurrentPageSlug().currentPageSlug.value = 'acme'
    try {
      const { wrapper, router } = await mountLayout()

      await wrapper.find('.login-button').trigger('click')
      await flushPromises()

      expect(router.currentRoute.value.fullPath).toBe('/login?page=acme')
    } finally {
      useCurrentPageSlug().currentPageSlug.value = null
    }
  })

  it('zeigt statt des Login-Buttons das Benutzermenü, wenn ein User eingeloggt ist', async () => {
    useAuth().setAuth('jwt-1', { email: 'user@x.de', role: 'Viewer' })
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.login-button').exists()).toBe(false)
    expect(wrapper.find('.user-menu-trigger').exists()).toBe(true)
    expect(wrapper.find('.user-menu-header strong').text()).toBe('user@x.de')
    expect(wrapper.find('.user-menu-header small').text()).toBe('(Viewer)')
  })

  it('blendet den Dashboard-Menüpunkt nur mit admin_dashboard-Permission ein', async () => {
    useTenant().setMyPages([])
    useTenant().setActiveTenant(null)
    useAuth().setAuth('jwt-1', { email: 'a@x.de', permissions: '' })
    const { wrapper: withoutPerm } = await mountLayout()
    expect(withoutPerm.text()).not.toContain('Dashboard')

    useAuth().logout()
    setAuthAsPageRole('jwt-2', { email: 'b@x.de', permissions: 'admin_dashboard' })
    const { wrapper: withPerm } = await mountLayout()
    expect(withPerm.text()).toContain('Dashboard')
  })

  it('wertet admin_dashboard der globalen Rolle nicht aus, nur das Recht auf der aktiven Seite', async () => {
    useTenant().setMyPages([])
    useTenant().setActiveTenant(null)
    useAuth().setAuth('jwt-1', { email: 'a@x.de', permissions: 'admin_dashboard' })
    const { wrapper } = await mountLayout()
    expect(wrapper.text()).not.toContain('Dashboard')
  })

  // Der Dashboard-Link darf nicht davon abhängen, ob StatusPage.vue
  // zuvor einen Tenant aktiviert hat. Auf Startseite/Bestellung lädt das Layout
  // die eigenen Seiten selbst und zeigt den Link, sobald eine Seite
  // admin_dashboard gewährt.
  describe('Dashboard-Link ohne aktiven Tenant', () => {
    const mockPages = (pages) => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => pages }))
      globalThis.fetch = fetchMock
      return fetchMock
    }
    const dashboardItem = (wrapper) => wrapper.findAll('.user-menu-item').find(el => el.text().includes('Dashboard'))

    beforeEach(() => {
      useTenant().setMyPages([])
      useTenant().setActiveTenant(null)
    })

    it('zeigt den Link, lädt die Seiten selbst und wählt beim Klick die erste passende Seite', async () => {
      const fetchMock = mockPages([
        { ID: 4, Permissions: 'components:read' },
        { ID: 7, Permissions: 'admin_dashboard,components:read' },
        { ID: 9, Permissions: 'admin_dashboard' },
      ])
      useAuth().setAuth('jwt-1', { email: 'a@x.de', role: 'Viewer' })
      const { wrapper, router } = await mountLayout()

      expect(fetchMock.mock.calls[0][0]).toContain('/api/me/pages')
      const item = dashboardItem(wrapper)
      expect(item).toBeTruthy()
      await item.trigger('click')
      await flushPromises()
      expect(String(useTenant().activeTenantId.value)).toBe('7')
      expect(router.currentRoute.value.path).toBe('/admin')
    })

    it('behält einen aktiven Tenant, der admin_dashboard gewährt', async () => {
      mockPages([
        { ID: 7, Permissions: 'admin_dashboard' },
        { ID: 9, Permissions: 'admin_dashboard' },
      ])
      useTenant().setActiveTenant(9)
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper } = await mountLayout()

      await dashboardItem(wrapper).trigger('click')
      await flushPromises()
      expect(String(useTenant().activeTenantId.value)).toBe('9')
    })

    it('zeigt keinen Link, wenn keine Seite admin_dashboard gewährt', async () => {
      mockPages([{ ID: 4, Permissions: 'components:read' }])
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper } = await mountLayout()
      expect(dashboardItem(wrapper)).toBeUndefined()
    })

    it('zeigt keinen Link, wenn das Laden der Seiten fehlschlägt', async () => {
      globalThis.fetch = vi.fn(async () => { throw new Error('offline') })
      vi.spyOn(console, 'error').mockImplementation(() => {})
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper } = await mountLayout()
      expect(dashboardItem(wrapper)).toBeUndefined()
    })

    it('zeigt Global Admins den Link auch ohne Seitenrolle', async () => {
      mockPages([])
      useAuth().setAuth('jwt-1', { email: 'a@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()
      expect(dashboardItem(wrapper)).toBeTruthy()
    })

    it('lädt beim Nutzerwechsel neu und zeigt nicht den Link anhand der Seiten des Vorgängers', async () => {
      const fetchMock = mockPages([{ ID: 7, Permissions: 'admin_dashboard' }])
      useAuth().setAuth('jwt-a', { email: 'a@x.de' })
      const { wrapper } = await mountLayout()
      expect(dashboardItem(wrapper)).toBeTruthy()

      useAuth().logout()
      await flushPromises()
      fetchMock.mockImplementation(async () => ({ ok: true, status: 200, json: async () => [] }))
      fetchMock.mockClear()
      useAuth().setAuth('jwt-b', { email: 'b@x.de' })
      await flushPromises()

      // Frühere Layouts anderer Tests hängen am selben Nutzer, daher mindestens ein Abruf.
      expect(fetchMock).toHaveBeenCalled()
      expect(dashboardItem(wrapper)).toBeUndefined()
      expect(useTenant().activeTenantId.value).toBeNull()
    })

    it('Gegenrichtung: Nutzer B mit Recht sieht den Link, obwohl A keines hatte', async () => {
      const fetchMock = mockPages([{ ID: 4, Permissions: 'components:read' }])
      useAuth().setAuth('jwt-a', { email: 'a@x.de' })
      const { wrapper } = await mountLayout()
      expect(dashboardItem(wrapper)).toBeUndefined()

      useAuth().logout()
      fetchMock.mockImplementation(async () => ({ ok: true, status: 200, json: async () => [{ ID: 9, Permissions: 'admin_dashboard' }] }))
      useAuth().setAuth('jwt-b', { email: 'b@x.de' })
      await flushPromises()
      expect(dashboardItem(wrapper)).toBeTruthy()
    })

    it('verwirft eine späte Antwort, wenn der Nutzer inzwischen abgemeldet ist', async () => {
      let resolve
      globalThis.fetch = vi.fn(() => new Promise((r) => { resolve = r }))
      useAuth().setAuth('jwt-a', { email: 'a@x.de' })
      await mountLayout()

      useAuth().logout()
      resolve({ ok: true, status: 200, json: async () => [{ ID: 7, Permissions: 'admin_dashboard' }] })
      await flushPromises()
      expect(useTenant().myPages.value).toEqual([])
    })

    it('Statusseite X ohne Recht, Recht auf Seite Y: Link da, Klick schaltet auf Y', async () => {
      mockPages([
        { ID: 1, Permissions: 'components:read' },
        { ID: 2, Permissions: 'admin_dashboard' },
      ])
      useTenant().setActiveTenant(1)
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper, router } = await mountLayout()

      await dashboardItem(wrapper).trigger('click')
      await flushPromises()
      expect(String(useTenant().activeTenantId.value)).toBe('2')
      expect(router.currentRoute.value.path).toBe('/admin')
    })

    it('lädt und zeigt nichts, solange niemand angemeldet ist', async () => {
      const fetchMock = mockPages([{ ID: 7, Permissions: 'admin_dashboard' }])
      const { wrapper } = await mountLayout()
      expect(fetchMock).not.toHaveBeenCalled()
      expect(wrapper.text()).not.toContain('Dashboard')
    })
  })

  it('navigiert beim Klick auf Dashboard zu /admin', async () => {
    setAuthAsPageRole('jwt-1', { email: 'a@x.de', permissions: 'admin_dashboard' })
    const { wrapper, router } = await mountLayout()

    const items = wrapper.findAll('.user-menu-item')
    const dashboardItem = items.find(el => el.text().includes('Dashboard'))
    await dashboardItem.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/admin')
  })

  it('dispatcht ein statushafen:open-subscription-manager-Event beim Klick auf Abonnement verwalten', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    const { wrapper } = await mountLayout()
    const listener = vi.fn()
    window.addEventListener('statushafen:open-subscription-manager', listener)

    const items = wrapper.findAll('.user-menu-item')
    const subscriptionItem = items.find(el => el.text().includes('Abonnement verwalten'))
    await subscriptionItem.trigger('click')

    expect(listener).toHaveBeenCalledTimes(1)
    window.removeEventListener('statushafen:open-subscription-manager', listener)
  })

  it('meldet den User beim Klick auf Abmelden ab und navigiert auf /', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    const { wrapper, router } = await mountLayout()
    await router.push('/admin/components')
    await flushPromises()

    const items = wrapper.findAll('.user-menu-item')
    const logoutItem = items.find(el => el.classes().includes('user-menu-item--danger'))
    await logoutItem.trigger('click')
    await flushPromises()

    expect(useAuth().isLoggedIn.value).toBe(false)
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('schaltet das Theme beim Klick auf den Theme-Button um', async () => {
    const { wrapper } = await mountLayout()
    const before = document.documentElement.dataset.theme

    await wrapper.find('.theme-toggle').trigger('click')
    await flushPromises()

    expect(document.documentElement.dataset.theme).not.toBe(before)
  })

  it('gibt Theme-Knopf, Sprachauswahl und Benutzer-Knopf die gemeinsame Klasse header-control', async () => {
    useAuth().setAuth('jwt-1', { email: 'user@x.de', role: 'Viewer' })
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.theme-toggle').classes()).toContain('header-control')
    expect(wrapper.find('.language-switcher').classes()).toContain('header-control')
    expect(wrapper.find('.user-menu-trigger').classes()).toContain('header-control')
  })

  it('gibt Theme- und Benutzer-Knopf dieselbe Größenklasse header-control--icon', async () => {
    useAuth().setAuth('jwt-1', { email: 'user@x.de', role: 'Viewer' })
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.theme-toggle').classes()).toContain('header-control--icon')
    expect(wrapper.find('.user-menu-trigger').classes()).toContain('header-control--icon')
    expect(wrapper.find('.language-switcher').classes()).not.toContain('header-control--icon')
  })

  // Die Kopfzeile wirkte wuchtiger als die Vorlage (30-36 px), weil die
  // 44-px-Zielfläche auch sichtbar war. Zielfläche bleibt 44 px, sichtbar
  // sind 44 - 2 * Ring px; Hintergrund wird auf die Innenfläche begrenzt.
  it('zeichnet Kopfzeilen-Bedienelemente höchstens 36 px hoch, Zielfläche bleibt 44 px', () => {
    const size = Number(layoutSource.match(/--header-control-size:\s*(\d+)px/)[1])
    const ring = Number(layoutSource.match(/--header-control-ring:\s*(\d+)px/)[1])
    expect(size).toBeGreaterThanOrEqual(44)
    expect(size - 2 * ring).toBeLessThanOrEqual(36)
    const control = layoutSource.match(/\n\.header-control\s*\{([^}]*)\}/)[1]
    expect(control).toMatch(/border:\s*var\(--header-control-ring\) solid transparent/)
    expect(control).toMatch(/background-clip:\s*padding-box/)
    const select = layoutSource.match(/\.header-control\.language-switcher\) \{([^}]*)\}/)[1]
    expect(select).toMatch(/border:\s*var\(--header-control-ring\) solid transparent/)
    const order = layoutSource.match(/\.app-header \.order-button \{([^}]*)\}/)[1]
    expect(order).toMatch(/min-height:\s*var\(--header-control-size\)/)
    expect(order).toMatch(/background-clip:\s*padding-box/)
  })

  it('gibt allen Kopfzeilen-Knöpfen über header-control dieselbe Höhe', () => {
    expect(layoutSource).toMatch(/--header-control-size:\s*44px/)
    expect(layoutSource.match(/\.header-control\s*\{([^}]*)\}/)[1]).toContain('height: var(--header-control-size)')
    expect(layoutSource.match(/\.header-control--icon\s*\{([^}]*)\}/)[1]).toContain('width: var(--header-control-size)')
  })

  it('setzt keinen Inline-Style am Theme-Knopf', async () => {
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.theme-toggle').attributes('style')).toBeUndefined()
  })

  it('verwendet die action-btn-Klasse nicht mehr für Kopfzeilen-Knöpfe', async () => {
    useAuth().setAuth('jwt-1', { email: 'user@x.de', role: 'Viewer' })
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.theme-toggle').classes()).not.toContain('action-btn')
    expect(wrapper.find('.user-menu-trigger').classes()).not.toContain('action-btn')
  })

  it('zeigt die Rolle in Klammern, wenn sie gesetzt ist', async () => {
    useAuth().setAuth('jwt-1', { email: 'user@x.de', role: 'admin' })
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.user-menu-header small').text()).toBe('(admin)')
  })

  it.each([['leer', ''], ['null', null], ['undefined', undefined]])(
    'lässt die Rollenzeile weg, wenn die Rolle %s ist',
    async (_name, role) => {
      useAuth().setAuth('jwt-1', { email: 'user@x.de', role })
      const { wrapper } = await mountLayout()

      expect(wrapper.find('.user-menu-header strong').text()).toBe('user@x.de')
      expect(wrapper.find('.user-menu-header small').exists()).toBe(false)
    },
  )

  it('setzt die App-Schrift über die Klasse user-menu und das Stylesheet', async () => {
    useAuth().setAuth('jwt-1', { email: 'user@x.de', role: 'admin' })
    const { wrapper } = await mountLayout()
    const menu = wrapper.find('ul.user-menu')

    expect(menu.exists()).toBe(true)
    expect(menu.element.contains(wrapper.find('.user-menu-header strong').element)).toBe(true)
    expect(menu.element.contains(wrapper.find('.user-menu-header small').element)).toBe(true)
    expect(layoutSource).toMatch(/\.user-menu\s*\{[^}]*font-family:\s*var\(--font-sans\)/)
  })

  it('definiert einheitliche Größe und sichtbaren Tastaturfokus für header-control', () => {
    expect(layoutSource).toMatch(/--header-control-size:\s*(44|40)px/)
    expect(layoutSource).toMatch(/\.header-control\s*\{[^}]*box-sizing:\s*border-box/)
    expect(layoutSource).toMatch(/\.header-control:focus-visible\s*\{[^}]*outline/)
    expect(layoutSource).toMatch(/\.header-control--icon\s*\{[^}]*width:\s*var\(--header-control-size\)/)
  })

  // Footer mit Link zur Dokumentation und Copyright.
  it('zeigt einen Footer mit Link zur Dokumentation, der nach /docs führt', async () => {
    const { wrapper, router } = await mountLayout()

    const footer = wrapper.find('footer.app-footer')
    expect(footer.exists()).toBe(true)
    expect(footer.find('nav').attributes('aria-label')).toBe('Fußzeile')
    const link = footer.find('.footer-links a')
    expect(link.text()).toBe('Dokumentation')
    expect(link.attributes('href')).toBe('/docs')

    await link.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs')
  })

  it('zeigt im Footer das Copyright mit dem aktuellen Jahr und übersetzt den Link', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2031-05-01T12:00:00Z'))
    try {
      const { wrapper } = await mountLayout()
      expect(wrapper.find('.footer-copyright').text()).toBe('© 2031 Statushafen')

      i18n.global.locale.value = 'en'
      await flushPromises()
      expect(wrapper.find('.footer-links a').text()).toBe('Documentation')
    } finally {
      vi.useRealTimers()
    }
  })

  // Footer in derselben Breite wie die Kopfzeile: Er liegt im selben
  // .app-container und nutzt für Farben nur Tokens (hell und dunkel).
  it('legt den Footer in den Layout-Container und färbt ihn nur über Tokens', async () => {
    const { wrapper } = await mountLayout()
    const container = wrapper.find('.app-container')
    expect(container.find(':scope > footer.app-footer').exists()).toBe(true)
    expect(container.find(':scope > header.app-header').exists()).toBe(true)
    const rule = layoutSource.match(/\.app-footer \{([^}]*)\}/)?.[1] ?? ''
    expect(rule).toMatch(/border-top: 1px solid var\(--border\)/)
    expect(rule).toMatch(/color: var\(--text-2\)/)
    expect(rule).not.toMatch(/#[0-9a-f]{3,6}/i)
  })

  // Richtung A: Die volle Kopfzeile gilt nur auf der Startseite.
  // Sprachauswahl der Startseite zeigte den Browser-Pfeil und sah neben Theme-Knopf und Avatar fremd aus.
  describe('Sprachauswahl der Kopfzeile: eigener Pfeil', () => {
    const rule = layoutSource.match(/\.app-header :deep\(\.header-control\.language-switcher\)\s*\{([^}]*)\}/)[1]

    it('schaltet das Browser-Aussehen ab und lässt Platz für den Pfeil', () => {
      expect(rule).toMatch(/appearance:\s*none/)
      expect(rule).toMatch(/padding-right:\s*28px/)
    })

    it('zeichnet den Pfeil als zwei Diagonalen in currentColor, ohne den Hintergrund zu überschreiben', () => {
      expect(rule.match(/linear-gradient\(/g)).toHaveLength(2)
      expect(rule).toContain('currentColor')
      expect(rule).toMatch(/background-color:\s*var\(--surface\)/)
      expect(rule).not.toMatch(/background:\s/)
    })
  })

  describe('Kopfzeile der Startseite', () => {
    it('Statusseiten und übrige Seiten behalten den schlichten Kopf', async () => {
      const { wrapper } = await mountLayout()
      expect(wrapper.find('.app-container').classes()).not.toContain('app-container--landing')
      expect(wrapper.find('header').classes()).not.toContain('app-header--landing')
      expect(wrapper.find('.lp-nav').exists()).toBe(false)
      expect(wrapper.find('.order-button').exists()).toBe(false)
      expect(wrapper.find('svg.logo-mark').exists()).toBe(true)
    })

    it('zeigt auf der Startseite Marker, Navigation, Anmelden und Bestell-Knopf, ohne „Preise“', async () => {
      useLandingChrome().isLanding.value = true
      const { wrapper } = await mountLayout()
      expect(wrapper.find('.app-container').classes()).toContain('app-container--landing')
      expect(wrapper.find('svg.logo-mark').exists()).toBe(true)
      expect(wrapper.find('img.logo-icon').exists()).toBe(false)
      const nav = wrapper.find('nav.lp-nav')
      expect(nav.attributes('aria-label')).toBe('Hauptmenü')
      expect(nav.findAll('a').map(a => [a.text(), a.attributes('href')])).toEqual([
        ['Funktionen', '#features'],
        ['Statusseiten', '#directory'],
        ['Dokumentation', '/docs'],
      ])
      expect(nav.text()).not.toMatch(/Preis/)
      expect(wrapper.find('.login-button').text()).toBe('Anmelden')
      const order = wrapper.find('a.order-button')
      expect(order.text()).toBe('Statusseite bestellen')
      expect(order.attributes('href')).toBe('/order')
      // Theme-Umschalter und Sprachauswahl bleiben erhalten.
      expect(wrapper.find('.theme-toggle').exists()).toBe(true)
      expect(wrapper.find('.language-switcher').exists()).toBe(true)
    })

    it('blendet den Bestell-Knopf der Kopfzeile aus, wenn die Bestellung deaktiviert ist', async () => {
      useLandingChrome().isLanding.value = true
      useLandingChrome().ordersEnabled.value = false
      const { wrapper } = await mountLayout()
      expect(wrapper.find('.order-button').exists()).toBe(false)
      expect(wrapper.find('.lp-nav').exists()).toBe(true)
    })

    it('hat keine axe-Funde', async () => {
      useLandingChrome().isLanding.value = true
      const { wrapper } = await mountLayout()
      expect(await axe(wrapper)).toHaveNoViolations()
    })

    it('Kopf klebt, ist randlos und bricht mobil um (Quelltext, happy-dom rechnet kein Layout)', () => {
      const head = layoutSource.match(/\.app-header--landing \{([^}]*)\}/)?.[1] ?? ''
      expect(head).toMatch(/position:\s*sticky/)
      expect(head).toMatch(/border-bottom:\s*1px solid var\(--border\)/)
      expect(head).toMatch(/backdrop-filter/)
      expect(layoutSource).toMatch(/\.app-container\.app-container--landing \{[^}]*max-width:\s*none/)
      const inner = layoutSource.match(/\.app-header--landing \.header-inner \{([^}]*)\}/)?.[1] ?? ''
      expect(inner).toMatch(/max-width:\s*var\(--lp-max\)/)
      const media = layoutSource.match(/@media \(max-width: 640px\) \{([\s\S]*?)\n\}/g)?.at(-1) ?? ''
      expect(media).toMatch(/flex-wrap:\s*wrap/)
      expect(media).toMatch(/\.lp-nav \{[^}]*flex:\s*1 0 100%/)
    })
  })

  // Die Kopfzeile lief bei 400 px um 39 px seitlich über, weil die
  // Bedienelemente (Thema, Sprache, Anmelden = 287 px) nicht umbrachen und der
  // Inline-Style der .nav-actions kein flex-wrap kannte. happy-dom rechnet kein
  // Layout; die Messung im echten Browser steht in e2e/public-schmal.spec.js.
  describe('schmale Breiten', () => {
    it('.nav-actions und .app-header brechen um, ohne Inline-Style', async () => {
      const { wrapper } = await mountLayout()
      expect(wrapper.find('.nav-actions').attributes('style')).toBeUndefined()
      const actions = layoutSource.match(/^\.nav-actions \{([^}]*)\}/m)?.[1] ?? ''
      expect(actions).toMatch(/flex-wrap:\s*wrap/)
      expect(actions).toMatch(/min-width:\s*0/)
      expect(actions).toMatch(/gap:\s*15px/)
      const header = layoutSource.match(/^\.app-header \{([^}]*)\}/m)?.[1] ?? ''
      expect(header).toMatch(/flex-wrap:\s*wrap/)
    })
  })

  // Auf der Anmeldeseite selbst ist der Kopfzeilen-Knopf „Anmelden“ überflüssig.
  it('zeigt auf der Anmeldeseite keinen Anmelden-Knopf, auf anderen Seiten schon', async () => {
    const { wrapper, router } = await mountLayout()
    expect(wrapper.find('.login-button').exists()).toBe(true)
    await router.push('/login')
    await flushPromises()
    expect(wrapper.find('.login-button').exists()).toBe(false)
    await router.push('/docs')
    await flushPromises()
    expect(wrapper.find('.login-button').exists()).toBe(true)
  })

  it('hat keine axe-Funde, ausgeloggt und mit Benutzermenü', async () => {
    const { wrapper: anonymous } = await mountLayout()
    expect(await axe(anonymous)).toHaveNoViolations()
    anonymous.unmount()

    setAuthAsPageRole('jwt-1', { email: 'a@x.de', permissions: 'admin_dashboard' })
    const { wrapper } = await mountLayout()
    expect(wrapper.find('.user-menu-item').exists()).toBe(true)
    expect(await axe(wrapper)).toHaveNoViolations()
    wrapper.unmount()
  })
})
