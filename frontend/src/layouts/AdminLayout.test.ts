import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { h } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { setAuthAsPageRole } from '../test-support/pageRole'
import { useTenant } from '../composables/useTenant'
import AppIcon from '../components/AppIcon.vue'
import AdminLayout from './AdminLayout.vue'
import { axe } from '../test-support/axe'
import { asResponse, mockedFetch } from '../test-support/fetchStub'
import type { AuthUser, MyPage } from '../types/api'
import { NAV_GROUPS } from '../utils/adminNav'

const Empty = { template: '<div />' }

// A page that reports its loading; this way we can check that AdminLayout does not
// even build it without read permission (no API calls of the page).
let probeMounted = 0
const Probe = {
  setup() {
    probeMounted++
    return () => h('div', { class: 'probe' }, 'Seiteninhalt')
  },
}

// All 'to' targets of AdminLayout.vue's navGroups must be resolvable in the test router,
// otherwise vue-router warns loudly on every <router-link>
// "No match found for location" (not an error, but needless test output noise).
function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Empty },
      { path: '/status/:slug', component: Empty },
      // Like router/index.js: start page (meta.home) and tabs with meta.permission.
      { path: '/admin', component: Empty, meta: { home: true } },
      ...NAV_GROUPS.flatMap(g => g.items).filter(i => i.resource && i.to !== '/admin/overview' && i.to !== '/admin/components')
        .map(i => ({ path: i.to, component: Probe, meta: { permission: `${i.resource}:read`, ...(i.globalOnly ? { globalOnly: true } : {}) } })),
      { path: '/admin/overview', component: Probe, meta: { permission: 'overview:read' } },
      { path: '/admin/components', component: Probe, meta: { permission: 'components:read' } },
      { path: '/admin/profile', component: Empty },
      { path: '/admin/docs/:section?', component: Empty },
      { path: '/admin/incidents/:incidentId/translations', component: Empty },
      { path: '/admin/components/:componentId/translations', component: Empty },
      { path: '/admin/notices/:noticeId/translations', component: Empty },
    ],
  })
}

// Permissions come from the role of the active page; the layout loads them
// itself from /api/me/pages, so the mock returns the same pages.
const pagesNow = () => JSON.parse(JSON.stringify(useTenant().myPages.value))
function asPageRole(token: string, user: AuthUser) {
  setAuthAsPageRole(token, user)
  global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => pagesNow() })
}

// useTenant/useAuth hold module-global state. A layout that is not torn down
// reacts to EVERY later change (activeTenantId, myPages, route) with watchers and
// network calls. With ~80 tests up to 80 live layouts piled up; the tests
// got slower per run (up to over 13 s) and ran into the timeout in the full run.
// So this helper remembers every instance and tears it down after each test.
const mounted: VueWrapper[] = []
function track(wrapper: VueWrapper) {
  const unmount = wrapper.unmount.bind(wrapper)
  let done = false
  wrapper.unmount = () => { if (!done) { done = true; unmount() } }
  mounted.push(wrapper)
  return wrapper
}

async function mountLayout({ initialPath = '/admin/components', attach = false }: { initialPath?: string; attach?: boolean } = {}) {
  const router = makeRouter()
  router.push(initialPath)
  await router.isReady()

  const wrapper = track(mount(AdminLayout, {
    global: { plugins: [i18n, router] },
    // Focus tests need a real document (document.activeElement).
    ...(attach ? { attachTo: document.body } : {}),
  }))
  await flushPromises()
  return { wrapper, router }
}

describe('AdminLayout', () => {
  afterEach(() => {
    while (mounted.length) mounted.pop()!.unmount()
  })

  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    // Tests assert on the German wording of the translated nav labels;
    // without a stored preference detectInitialLocale() would otherwise fall back to 'en'
    // in the test environment (happy-dom navigator.language === 'en-US').
    i18n.global.locale.value = 'de'
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => [],
    })
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  // A torn-down layout must no longer react to the module-global state;
  // a leftover one reloads outages on every tenant switch and
  // made the late tests of the file so slow that they ran into the timeout.
  it('reagiert nach dem Abbauen nicht mehr auf den globalen Mandantenwechsel', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    const { wrapper } = await mountLayout()
    wrapper.unmount()
    wrapper.unmount() // unmounting twice (tests unmount themselves, afterEach again) is harmless
    mockedFetch().mockClear()

    useTenant().setActiveTenant(42)
    await flushPromises()

    expect(global.fetch).not.toHaveBeenCalled()
  })

  // Bell between the theme button and the language picker, only with the matching permission.
  it('zeigt die Nachrichtenglocke zwischen Theme-Knopf und Sprachwähler, Betrachtern nicht', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
    const { wrapper } = await mountLayout()
    const kids = Array.from(wrapper.find('.topbar-right').element.children).map(el => (el as Element).className.split(' ')[0])
    expect(kids.slice(0, 3)).toEqual(['icon-btn', 'notif', 'language-switcher-root'])
    wrapper.unmount()

    asPageRole('jwt-2', { email: 'v@x.de', is_global_admin: false, permissions: 'components:read' })
    const viewer = await mountLayout()
    expect(viewer.wrapper.find('.notif').exists()).toBe(false)
    viewer.wrapper.unmount()
  })

  // The "admin" token layer (Direction C, tokens.css) hangs on this class.
  it('trägt die Token-Ebene theme-admin am Wurzelelement, nicht theme-public', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
    const { wrapper } = await mountLayout()
    expect(wrapper.classes()).toContain('theme-admin')
    expect(wrapper.classes()).not.toContain('theme-public')
  })

  it('zeigt admin-only Navigationseinträge nur für Global Admins', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
    const { wrapper } = await mountLayout()

    const labels = wrapper.findAll('.area-items a').map(a => a.text())
    expect(labels).toContain('Übersicht')
    expect(labels).toContain('Statusseiten')
    expect(labels).toContain('Benutzer')
  })

  it('blendet admin-only und perm-geschützte Einträge für einfache User aus', async () => {
    asPageRole('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: 'view' })
    const { wrapper } = await mountLayout()

    const labels = wrapper.findAll('.area-items a').map(a => a.text())
    expect(labels).not.toContain('Übersicht')
    expect(labels).not.toContain('Benutzer')
    expect(labels).not.toContain('Ankündigungen')
    expect(labels).not.toContain('Abonnenten')
    expect(labels).not.toContain('Komponenten')
    expect(labels).not.toContain('Vorfälle')
    expect(labels).not.toContain('Wartungsfenster')
    // Templates have their own read permission (incident_templates:read)
    expect(labels).not.toContain('Vorlagen')
    // Entries without perm/adminOnly stay visible
    expect(labels).toContain('Dokumentation')
  })

  it('zeigt Komponenten, Vorfälle und Wartungsfenster mit dem Leserecht', async () => {
    asPageRole('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: 'components:read,incidents:read,maintenance:read' })
    const { wrapper } = await mountLayout()

    const labels = wrapper.findAll('.area-items a').map(a => a.text())
    expect(labels).toContain('Komponenten')
    expect(labels).toContain('Vorfälle')
    expect(labels).toContain('Wartungsfenster')
  })

  it('verlinkt Komponenten, Vorfälle und Wartungsfenster mit Icon', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
    // Start path away from the three targets, so an empty `to` (relative to the current route) stands out.
    const { wrapper } = await mountLayout({ initialPath: '/admin/tags' })

    for (const [href, label, icon] of [['/admin/components', 'Komponenten', 'grid'], ['/admin/incidents', 'Vorfälle', 'alert-triangle'], ['/admin/maintenance', 'Wartungsfenster', 'clock']]) {
      const link = wrapper.findAll('.area-items a').find(a => a.text() === label)
      expect(link!.attributes('href')).toBe(href)
      const expectedIcon = mount(AppIcon, { props: { name: icon, size: 16 } })
      expect(link!.find('svg').html()).toBe(expectedIcon.html())
    }
  })

  // The backend allows the SSO configuration with sso:read on
  // the page; the menu item was nevertheless only visible to global admins.
  it('zeigt SSO mit sso:read auf der aktiven Seite, ohne Berechtigung nicht', async () => {
    asPageRole('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: 'view' })
    useTenant().setMyPages([{ ID: 3, Name: 'Seite', Permissions: 'sso:read' }] as MyPage[])
    useTenant().setActiveTenant(3)
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [{ ID: 3, Name: 'Seite', Permissions: 'sso:read' }] })
    const { wrapper } = await mountLayout()
    const link = wrapper.findAll('.area-items a').find(a => a.attributes('href') === '/admin/sso')
    expect(link?.exists()).toBe(true)

    useTenant().setMyPages([{ ID: 3, Name: 'Seite', Permissions: 'view' }] as MyPage[])
    await flushPromises()
    expect(wrapper.findAll('.area-items a').some(a => a.attributes('href') === '/admin/sso')).toBe(false)
  })

  it('zeigt nur den Menüpunkt, für den die Berechtigung vorhanden ist', async () => {
    asPageRole('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: 'components:read' })
    const { wrapper } = await mountLayout()

    const labels = wrapper.findAll('.area-items a').map(a => a.text())
    expect(labels).toContain('Komponenten')
    expect(labels).not.toContain('Vorfälle')
    expect(labels).not.toContain('Wartungsfenster')
  })

  // Navigation per seed role (permissions like database.SeedData); the
  // role hangs, as in reality, on the active status page (/api/me/pages).
  describe('Navigation je Rolle', () => {
    const ROLE_PERMS = {
      // The default roles as create/read/update/delete per tab (like database.SeedData).
      Viewer: 'components:read,external_sources:read,incidents:read,maintenance:read,notices:read',
      Editor: 'components:read,external_sources:read,incidents:create,incidents:read,incidents:update,incidents:delete,'
        + 'maintenance:create,maintenance:read,maintenance:update,maintenance:delete,'
        + 'incident_templates:create,incident_templates:read,incident_templates:update,incident_templates:delete,'
        + 'notices:create,notices:read,notices:update,notices:delete',
      'Page Admin': 'components:create,components:read,components:update,components:delete,'
        + 'external_sources:create,external_sources:read,external_sources:update,external_sources:delete,'
        + 'incidents:create,incidents:read,incidents:update,incidents:delete,'
        + 'maintenance:create,maintenance:read,maintenance:update,maintenance:delete,'
        + 'incident_templates:create,incident_templates:read,incident_templates:update,incident_templates:delete,'
        + 'notices:create,notices:read,notices:update,notices:delete,'
        + 'subscribers:create,subscribers:read,subscribers:update,subscribers:delete,admin_dashboard',
    }
    const ALL_ITEMS = ['Übersicht', 'Komponenten', 'Externe Quellen', 'Vorfälle', 'Wartungsfenster', 'Ankündigungen', 'Vorlagen',
      'Abonnenten', 'Mail-Templates', 'Sprachen', 'UI-Texte', 'Statusseiten', 'Benutzer', 'Rollen & Rechte',
      'Tags', 'Audit-Log', 'Monitoring', 'SSO Konfiguration', 'Systemeinstellungen', 'Dokumentation']

    async function mountAsPageRole(role: string) {
      const pages = [{ ID: 7, Name: 'Acme', RoleName: role, Permissions: ROLE_PERMS[role as keyof typeof ROLE_PERMS] }] as MyPage[]
      useAuth().setAuth('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: '' })
      useTenant().setMyPages(pages)
      useTenant().setActiveTenant(7)
      global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => pages })
      return mountLayout()
    }
    const navLabels = (wrapper: VueWrapper) => wrapper.findAll('.area-items a').map(a => a.text())
    const groupLabels = (wrapper: VueWrapper) => wrapper.findAll('.area-tab').map(g => g.text())

    it.each([
      ['Viewer', ['Komponenten', 'Externe Quellen', 'Vorfälle', 'Wartungsfenster', 'Ankündigungen', 'Dokumentation'], ['Betrieb', 'System']],
      ['Editor', ['Komponenten', 'Externe Quellen', 'Vorfälle', 'Wartungsfenster', 'Ankündigungen', 'Vorlagen', 'Dokumentation'], ['Betrieb', 'System']],
      ['Page Admin', ['Komponenten', 'Externe Quellen', 'Vorfälle', 'Wartungsfenster', 'Ankündigungen', 'Vorlagen', 'Abonnenten', 'Dokumentation'], ['Betrieb', 'System']],
    ])('%s sieht genau die lesbaren Bereiche', async (role, expected, groups) => {
      const { wrapper } = await mountAsPageRole(role)
      expect(navLabels(wrapper)).toEqual(expected)
      // Areas without a visible entry (content, management) have no tab, no empty <li>
      expect(groupLabels(wrapper)).toEqual(groups)
      expect(wrapper.findAll('.area-items li').length).toBe(expected.length)
    })

    it('Global Admin sieht alle Bereiche und alle Gruppen', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout()
      expect(navLabels(wrapper)).toEqual(ALL_ITEMS)
      expect(groupLabels(wrapper)).toEqual(['Betrieb', 'Inhalte', 'Verwaltung', 'System'])
    })

    it('markiert die Startseite /admin/components für einen Viewer als aktiv', async () => {
      const { wrapper } = await mountAsPageRole('Viewer')
      const active = wrapper.findAll('.area-items a.active').map(a => a.text())
      expect(active).toEqual(['Komponenten'])
    })
  })

  it('zeigt Komponenten, Vorfälle und Wartungsfenster für Global Admins', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
    const { wrapper } = await mountLayout()

    const labels = wrapper.findAll('.area-items a').map(a => a.text())
    expect(labels).toEqual(expect.arrayContaining(['Komponenten', 'Vorfälle', 'Wartungsfenster']))
  })

  it('zeigt perm-geschützte Einträge, wenn der User die passende Permission hat', async () => {
    asPageRole('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: 'notices:read,subscribers:read' })
    const { wrapper } = await mountLayout()

    const labels = wrapper.findAll('.area-items a').map(a => a.text())
    expect(labels).toContain('Ankündigungen')
    expect(labels).toContain('Abonnenten')
  })

  it('zeigt "Global Admin" als Rolle für Global Admins statt der seitenbezogenen Rolle', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, role: 'Viewer' })
    const { wrapper } = await mountLayout()

    await wrapper.find('.user-menu-button').trigger('click')
    expect(wrapper.find('.user-menu-info small').text()).toBe('Global Admin')
  })

  it('lädt die Statusseiten beim Mounten und wählt automatisch die erste als aktiven Tenant', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    mockedFetch().mockResolvedValue(asResponse({
      ok: true,
      status: 200,
      json: async () => ([{ ID: 5, Name: 'Page A', Permissions: 'components:read' }, { ID: 6, Name: 'Page B', Permissions: 'components:read' }]),
    }))

    const { wrapper } = await mountLayout()

    expect(wrapper.find('.tenant-select').exists()).toBe(true)
    expect(useTenant().activeTenantId.value).toBe(5)
  })

  // After a page SSO login /api/me/pages returns only the
  // SSO page. Another page stored from an earlier session
  // (localStorage) must then not stay active.
  it('ersetzt einen gespeicherten Tenant, der nicht in der Seitenliste steht, durch die erste Seite', async () => {
    useAuth().setAuth('jwt-1', { email: 'v@x.de' })
    useTenant().setActiveTenant('9')
    mockedFetch().mockResolvedValue(asResponse({
      ok: true,
      status: 200,
      json: async () => ([{ ID: 5, Name: 'Page A', Permissions: 'components:read' }]),
    }))

    await mountLayout()

    expect(useTenant().activeTenantId.value).toBe(5)
  })

  it('behält einen gespeicherten Tenant, der in der Seitenliste steht', async () => {
    useAuth().setAuth('jwt-1', { email: 'v@x.de' })
    useTenant().setActiveTenant('6')
    mockedFetch().mockResolvedValue(asResponse({
      ok: true,
      status: 200,
      json: async () => ([{ ID: 5, Name: 'Page A' }, { ID: 6, Name: 'Page B' }]),
    }))

    await mountLayout()

    expect(useTenant().activeTenantId.value).toBe('6')
  })

  it('zeigt ein sichtbares Label über dem Tenant-Switcher', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    mockedFetch().mockResolvedValue(asResponse({
      ok: true,
      status: 200,
      json: async () => ([{ ID: 5, Name: 'Page A' }]),
    }))

    const { wrapper } = await mountLayout()

    const label = wrapper.find('label.tenant-select-label')
    expect(label.exists()).toBe(true)
    expect(label.text()).toBe('Mandant')
    expect(label.attributes('for')).toBe('tenant-select')
    expect(wrapper.find('#tenant-select').exists()).toBe(true)
  })

  it('zeigt einen Hinweis statt des Tenant-Switchers, wenn keine Statusseiten vorhanden sind', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    const { wrapper } = await mountLayout()

    expect(wrapper.find('.tenant-select').exists()).toBe(false)
    expect(wrapper.text()).toContain('Keine Statusseiten')
  })

  describe('Statusseiten-Auswahl: Lade- und Fehlerzustand', () => {
    async function mountWithPagesResponse(respond: () => unknown) {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      global.fetch = vi.fn(async () => respond()) as unknown as typeof fetch
      const router = makeRouter()
      router.push('/admin/components')
      await router.isReady()
      const wrapper = track(mount(AdminLayout, { global: { plugins: [i18n, router] } }))
      await flushPromises()
      return wrapper
    }

    it('zeigt solange die Statusseiten laden einen Ladeindikator statt "Keine Statusseiten"', async () => {
      const wrapper = await mountWithPagesResponse(() => new Promise(() => {}))

      expect(wrapper.find('.tenant-switch [role="status"][aria-busy="true"]').exists()).toBe(true)
      expect(wrapper.text()).not.toContain('Keine Statusseiten')
      expect(wrapper.find('.tenant-select').exists()).toBe(false)
    })

    it('zeigt nach leerer Antwort "Keine Statusseiten" ohne Ladeindikator', async () => {
      const wrapper = await mountWithPagesResponse(() => ({ ok: true, status: 200, json: async () => [] }))

      expect(wrapper.text()).toContain('Keine Statusseiten')
      expect(wrapper.find('.tenant-switch [role="status"]').exists()).toBe(false)
    })

    it('zeigt nach Antwort mit Daten die Auswahl mit den Seiten', async () => {
      const wrapper = await mountWithPagesResponse(() => ({ ok: true, status: 200, json: async () => [{ ID: 5, Name: 'Acme Corp' }] }))

      expect(wrapper.find('.tenant-select').text()).toContain('Acme Corp')
      expect(wrapper.text()).not.toContain('Keine Statusseiten')
      expect(wrapper.find('.tenant-switch [role="status"]').exists()).toBe(false)
    })

    it('zeigt bei einem Ladefehler einen Fehlerhinweis statt "Keine Statusseiten"', async () => {
      const wrapper = await mountWithPagesResponse(() => ({ ok: false, status: 500, json: async () => ({ error: 'kaputt' }) }))

      expect(wrapper.find('.tenant-switch [role="alert"]').text()).toBe('Statusseiten konnten nicht geladen werden.')
      expect(wrapper.text()).not.toContain('Keine Statusseiten')
      expect(wrapper.find('.tenant-switch [role="status"]').exists()).toBe(false)
    })
  })

  it('meldet den User beim Klick auf Abmelden ab und navigiert auf /', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    const { wrapper, router } = await mountLayout()

    await wrapper.find('.user-menu-button').trigger('click')
    await wrapper.find('.user-menu-link.is-danger').trigger('click')
    await flushPromises()

    expect(useAuth().isLoggedIn.value).toBe(false)
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('blendet das mobile Menü nach einem Routenwechsel wieder aus', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    const { wrapper, router } = await mountLayout()

    await wrapper.find('.burger-btn').trigger('click')
    expect(wrapper.find('.admin-nav').classes()).toContain('is-open')

    await router.push('/admin/overview')
    await flushPromises()

    expect(wrapper.find('.admin-nav').classes()).not.toContain('is-open')
  })

  describe('Phase 7b: Mobile Sidebar Scroll-Lock & Keyboard Navigation', () => {
    afterEach(() => {
      document.body.style.overflow = ''
    })

    it('sperrt das Body-Scrolling, wenn das mobile Menü geöffnet wird', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()

      expect(document.body.style.overflow).not.toBe('hidden')

      await wrapper.find('.burger-btn').trigger('click')
      await flushPromises()

      expect(document.body.style.overflow).toBe('hidden')
    })

    it('gibt das Body-Scrolling wieder frei, wenn das mobile Menü geschlossen wird', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()

      await wrapper.find('.burger-btn').trigger('click')
      await flushPromises()
      expect(document.body.style.overflow).toBe('hidden')

      await wrapper.find('.burger-btn').trigger('click')
      await flushPromises()

      expect(document.body.style.overflow).not.toBe('hidden')
    })

    it('schließt das mobile Menü und gibt das Scrolling frei, wenn die Komponente unmounted wird', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()

      await wrapper.find('.burger-btn').trigger('click')
      await flushPromises()
      expect(document.body.style.overflow).toBe('hidden')

      wrapper.unmount()

      expect(document.body.style.overflow).not.toBe('hidden')
    })

    it('schließt das mobile Menü bei Druck auf ESC', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()

      await wrapper.find('.burger-btn').trigger('click')
      await flushPromises()
      expect(wrapper.find('.admin-nav').classes()).toContain('is-open')

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await flushPromises()

      expect(wrapper.find('.admin-nav').classes()).not.toContain('is-open')
      expect(document.body.style.overflow).not.toBe('hidden')
    })

    it('ignoriert ESC, wenn das mobile Menü bereits geschlossen ist', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()

      expect(wrapper.find('.admin-nav').classes()).not.toContain('is-open')

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await flushPromises()

      expect(wrapper.find('.admin-nav').classes()).not.toContain('is-open')
    })
  })

  // Link to the documentation in the system area, for everyone signed in.
  // Switch "show help tooltips" in the header's user menu,
  // takes effect immediately and is stored on the user.
  it('zeigt den Hilfe-Tooltips-Schalter im Benutzermenü und speichert ihn', async () => {
    useAuth().setAuth('jwt', { email: 'a@b.de', is_global_admin: true })
    const { wrapper } = await mountLayout()

    await wrapper.find('.user-menu-button').trigger('click')
    const toggle = wrapper.find('.user-menu-panel .user-menu-toggle')
    expect(toggle.text()).toContain('Hilfe-Tooltips anzeigen')
    await toggle.find('input[type="checkbox"]').setValue(false)
    expect(useAuth().currentUser.value!.help_tooltips).toBe(false)
    expect(global.fetch).toHaveBeenCalledWith('/api/me', expect.objectContaining({ method: 'PATCH', body: '{"help_tooltips":false}' }))
  })

  it('verlinkt die Dokumentation im Bereich System, auch ohne besondere Rechte', async () => {
    useAuth().setAuth('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: '' })
    const { wrapper, router } = await mountLayout()

    const link = wrapper.findAll('.area-items a').find(a => a.text() === 'Dokumentation')
    expect(link).toBeTruthy()
    // The docs stay inside the admin frame instead of leaving for the public /docs page.
    expect(link!.attributes('href')).toBe('/admin/docs')
    expect(link!.find('svg.app-icon').exists()).toBe(true)

    await link!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/docs')
    wrapper.unmount()
  })

  it('hat keine axe-Funde, auch mit geöffnetem Mobil- und Benutzermenü', async () => {
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    const { wrapper } = await mountLayout()
    expect(await axe(wrapper)).toHaveNoViolations()

    await wrapper.find('.burger-btn').trigger('click')
    expect(await axe(wrapper)).toHaveNoViolations()
    await wrapper.find('.burger-btn').trigger('click')

    await wrapper.find('.user-menu-button').trigger('click')
    expect(await axe(wrapper)).toHaveNoViolations()
    wrapper.unmount()
  })

  // Redesign stage 2: area tabs instead of a sidebar.
  // Outage banner for permanently failed external sources.
  describe('Ausfall-Banner externer Quellen', () => {
    const outage = { source_id: 3, source_name: 'Example Cloud', base_url: 'https://status.example.com', since: '2026-10-29T11:10:00Z', last_error: 'HTTP 503' }
    function mockOutages(items: unknown, status = 200) {
      global.fetch = vi.fn(async (url: string) => {
        if (/external-outages$/.test(url)) return { ok: status < 400, status, json: async () => items }
        if (/\/api\/me\/pages$/.test(url)) return { ok: true, status: 200, json: async () => pagesNow() }
        return { ok: true, status: 200, json: async () => [] }
      }) as unknown as typeof fetch
    }
    const outageCalls = () => mockedFetch().mock.calls.filter(([u]) => /external-outages$/.test(String(u)))

    beforeEach(() => {
      useTenant().setActiveTenant(7)
      asPageRole('jwt-1', { email: 'pa@x.de', is_global_admin: false, permissions: 'external_sources:update' })
    })

    it('zeigt ein Banner mit role=alert, Quellenname, Fehler und Link auf die Quellenseite', async () => {
      mockOutages([outage])
      const { wrapper } = await mountLayout()
      const banner = wrapper.find('.outage-banner')
      expect(banner.attributes('role')).toBe('alert')
      expect(banner.text()).toContain('Example Cloud')
      expect(banner.text()).toContain('HTTP 503')
      expect(banner.find('a').attributes('href')).toBe('/admin/external-sources')
      expect(outageCalls()[0][0]).toBe('/api/pages/7/external-outages')
    })

    it('zeigt mehrere Ausfälle und rendert den Fehlertext der Fremdseite nur als Text', async () => {
      mockOutages([{ ...outage, last_error: '<img src=x onerror=alert(1)>' }, { ...outage, source_id: 4, source_name: 'GitHub', last_error: '' }])
      const { wrapper } = await mountLayout()
      expect(wrapper.findAll('.outage-banner li')).toHaveLength(2)
      expect(wrapper.find('.outage-banner').text()).toContain('<img src=x onerror=alert(1)>')
      expect(wrapper.find('.outage-banner img').exists()).toBe(false)
      expect(wrapper.find('.outage-banner').text()).toContain('kein Fehlertext')
    })

    it('zeigt kein Banner ohne Ausfall oder bei einem Ladefehler', async () => {
      mockOutages([])
      expect((await mountLayout()).wrapper.find('.outage-banner').exists()).toBe(false)
      mockOutages({ error: 'kaputt' }, 500)
      expect((await mountLayout()).wrapper.find('.outage-banner').exists()).toBe(false)
    })

    it('zeigt kein Banner bei einer unerwarteten Antwortform', async () => {
      mockOutages({ unexpected: true })
      expect((await mountLayout()).wrapper.find('.outage-banner').exists()).toBe(false)
    })

    it('verwirft eine späte Antwort der vorherigen Statusseite nach einem Mandantenwechsel', async () => {
      let release!: (value?: unknown) => void
      const late = new Promise(resolve => { release = resolve })
      global.fetch = vi.fn(async (url: string) => {
        if (/pages\/7\/external-outages$/.test(url)) {
          await late
          return { ok: true, status: 200, json: async () => [outage] }
        }
        return { ok: true, status: 200, json: async () => [] }
      }) as unknown as typeof fetch
      const { wrapper } = await mountLayout()
      useTenant().setActiveTenant(8)
      await flushPromises()
      release()
      await flushPromises()
      expect(wrapper.find('.outage-banner').exists()).toBe(false)
    })

    it('fragt ohne external_sources:update gar nicht erst nach (wie die Ausfall-Mail)', async () => {
      asPageRole('jwt-1', { email: 'v@x.de', is_global_admin: false, permissions: 'components:read' })
      mockOutages([outage])
      const { wrapper } = await mountLayout()
      expect(outageCalls()).toHaveLength(0)
      expect(wrapper.find('.outage-banner').exists()).toBe(false)
    })

    it('verschwindet nach dem nächsten Abruf, sobald die Quelle wieder läuft', async () => {
      mockOutages([outage])
      const { wrapper, router } = await mountLayout()
      expect(wrapper.find('.outage-banner').exists()).toBe(true)
      mockOutages([])
      await router.push('/admin/incidents')
      await flushPromises()
      expect(wrapper.find('.outage-banner').exists()).toBe(false)
    })
  })

  describe('Bereichs-Reiter', () => {
    const tabs = (wrapper: VueWrapper) => wrapper.findAll('.area-tab')
    const shownItems = (wrapper: VueWrapper) => wrapper.findAll('.area-items.is-shown a').map(a => a.text())

    it('gliedert alle Einträge in die Bereiche Betrieb, Inhalte, Verwaltung und System', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout()

      const byArea = wrapper.findAll('.area-items').map(g => [g.find('.area-items-label').text(), g.findAll('a').map(a => a.text())])
      expect(byArea).toEqual([
        ['Betrieb', ['Übersicht', 'Komponenten', 'Externe Quellen', 'Vorfälle', 'Wartungsfenster', 'Ankündigungen', 'Vorlagen', 'Abonnenten']],
        ['Inhalte', ['Mail-Templates', 'Sprachen', 'UI-Texte']],
        ['Verwaltung', ['Statusseiten', 'Benutzer', 'Rollen & Rechte', 'Tags', 'Audit-Log']],
        ['System', ['Monitoring', 'SSO Konfiguration', 'Systemeinstellungen', 'Dokumentation']],
      ])
    })

    it('jeder Reiter führt auf den ersten Eintrag seines Bereichs', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout()

      expect(tabs(wrapper).map(a => [a.text(), a.attributes('href')])).toEqual([
        ['Betrieb', '/admin/overview'],
        ['Inhalte', '/admin/notification-templates'],
        ['Verwaltung', '/admin/pages'],
        ['System', '/admin/monitoring'],
      ])
    })

    it('führt den Reiter eines Bereichs ohne Übersicht auf den ersten sichtbaren Eintrag', async () => {
      asPageRole('jwt-1', { email: 'u@x.de', is_global_admin: false, permissions: 'incidents:read' })
      const { wrapper } = await mountLayout()

      expect(tabs(wrapper)[0].attributes('href')).toBe('/admin/incidents')
    })

    it('blendet Bereiche ohne sichtbaren Eintrag aus (nur Mail-Templates und Sprachen: Inhalte und System)', async () => {
      asPageRole('jwt-1', { email: 'u@x.de', is_global_admin: false, permissions: 'notification_templates:read,languages:update' })
      const { wrapper } = await mountLayout()

      expect(tabs(wrapper).map(a => a.text())).toEqual(['Inhalte', 'System'])
    })

    it('markiert den Bereich der aktuellen Route als aktiv und zeigt seine Einträge', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout({ initialPath: '/admin/tags' })

      const active = wrapper.findAll('.area-tab.active')
      expect(active.map(a => a.text())).toEqual(['Verwaltung'])
      expect(active[0].attributes('aria-current')).toBe('true')
      expect(tabs(wrapper).filter(a => !a.classes('active')).every(a => a.attributes('aria-current') === undefined)).toBe(true)
      expect(shownItems(wrapper)).toEqual(['Statusseiten', 'Benutzer', 'Rollen & Rechte', 'Tags', 'Audit-Log'])
      expect(wrapper.findAll('.area-items a.active').map(a => a.text())).toEqual(['Tags'])
    })

    it('ordnet Unterrouten (Übersetzungen eines Vorfalls) dem Bereich des Eintrags zu', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout({ initialPath: '/admin/incidents/5/translations' })

      expect(wrapper.findAll('.area-tab.active').map(a => a.text())).toEqual(['Betrieb'])
    })

    it('wechselt mit der Route den aktiven Bereich', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper, router } = await mountLayout({ initialPath: '/admin/tags' })

      await router.push('/admin/sso')
      await flushPromises()
      expect(wrapper.findAll('.area-tab.active').map(a => a.text())).toEqual(['System'])
      expect(shownItems(wrapper)).toContain('SSO Konfiguration')
    })

    it('zeigt ohne passenden Eintrag (Profil) keinen aktiven Reiter, aber den ersten Bereich', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout({ initialPath: '/admin/profile' })

      expect(wrapper.findAll('.area-tab.active')).toHaveLength(0)
      expect(wrapper.find('.area-items.is-shown .area-items-label').text()).toBe('Betrieb')
    })

    it('rendert für einen Benutzer ohne jedes Recht keinen Reiter außer System', async () => {
      useAuth().setAuth('jwt-1', { email: 'u@x.de', is_global_admin: false, permissions: '' })
      const { wrapper } = await mountLayout()

      expect(tabs(wrapper).map(a => a.text())).toEqual(['System'])
      expect(shownItems(wrapper)).toEqual(['Dokumentation'])
    })

    it('hat Marke, Navigation mit Namen und Reiter in der sticky Kopfzeile', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout()

      const header = wrapper.find('header.admin-topbar')
      expect(header.find('h1.topbar-brand .brand-name').text()).toBe('Statushafen')
      expect(header.find('h1.topbar-brand .brand-sub').text()).toBe('Admin')
      // Logo instead of a blue dot: decorative image (empty alt), the name is in the text
      const logo = header.find('h1.topbar-brand img.brand-logo')
      // In tests Vite inlines small files from public/ as a data: URL, in the build it stays /favicon.svg
      expect(logo.attributes('src')).toMatch(/favicon\.svg$|^data:image\/svg\+xml/)
      expect(logo.attributes('alt')).toBe('')
      expect(header.find('.brand-dot').exists()).toBe(false)
      expect(header.find('nav.admin-nav').attributes('aria-label')).toBe('Hauptnavigation')
      expect(header.find('.tenant-switch').exists()).toBe(true)
      expect(wrapper.find('aside').exists()).toBe(false)
    })
  })

  describe('Benutzermenü', () => {
    async function mountWithUser() {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const ctx = await mountLayout({ attach: true })
      return ctx
    }
    const button = (wrapper: VueWrapper) => wrapper.find('.user-menu-button')

    afterEach(() => {
      document.body.innerHTML = ''
    })

    it('ist geschlossen, trägt Kürzel und Namen und meldet aria-expanded', async () => {
      const { wrapper } = await mountWithUser()

      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      expect(button(wrapper).attributes('aria-expanded')).toBe('false')
      expect(button(wrapper).attributes('aria-label')).toBe('AD Benutzermenü: admin@x.de')
      expect(wrapper.find('.user-avatar').text()).toBe('AD')

      await button(wrapper).trigger('click')
      expect(wrapper.find('.user-menu-panel').exists()).toBe(true)
      expect(button(wrapper).attributes('aria-expanded')).toBe('true')
      expect(button(wrapper).attributes('aria-controls')).toBe(wrapper.find('.user-menu-panel').attributes('id'))
      wrapper.unmount()
    })

    it('zeigt E-Mail, Rolle, Hilfe-Schalter, Profil, Öffentliche Ansicht und Abmelden', async () => {
      const { wrapper } = await mountWithUser()
      await button(wrapper).trigger('click')

      const panel = wrapper.find('.user-menu-panel')
      expect(panel.find('.user-menu-info strong').text()).toBe('admin@x.de')
      expect(panel.find('.user-menu-info small').text()).toBe('Global Admin')
      expect(panel.text()).toContain('Hilfe-Tooltips anzeigen')
      expect(panel.findAll('.user-menu-link').map(l => l.text())).toEqual(['Profil', 'Öffentliche Ansicht', 'Abmelden'])
      expect(panel.find('a.user-menu-link').attributes('href')).toBe('/admin/profile')
      wrapper.unmount()
    })

    it('legt den Fokus beim Öffnen auf den ersten Eintrag', async () => {
      const { wrapper } = await mountWithUser()
      await button(wrapper).trigger('click')
      await flushPromises()

      expect(document.activeElement).toBe(wrapper.find('.user-menu-panel input[type="checkbox"]').element)
      wrapper.unmount()
    })

    it('öffnet per Pfeil nach unten am Knopf, ein zweiter Druck ändert nichts', async () => {
      const { wrapper } = await mountWithUser()
      await button(wrapper).trigger('keydown', { key: 'ArrowDown' })
      await flushPromises()
      expect(wrapper.find('.user-menu-panel').exists()).toBe(true)

      await button(wrapper).trigger('keydown', { key: 'ArrowDown' })
      expect(wrapper.find('.user-menu-panel').exists()).toBe(true)
      wrapper.unmount()
    })

    it('wandert mit Pfeiltasten, Home und Ende im Kreis durch die Einträge', async () => {
      const { wrapper } = await mountWithUser()
      await button(wrapper).trigger('click')
      await flushPromises()
      const panel = wrapper.find('.user-menu-panel')
      const [toggle, profile, publicView, logout] = [
        panel.find('input[type="checkbox"]').element,
        ...panel.findAll('.user-menu-link').map(l => l.element),
      ]

      await panel.trigger('keydown', { key: 'ArrowDown' })
      expect(document.activeElement).toBe(profile)
      await panel.trigger('keydown', { key: 'ArrowDown' })
      expect(document.activeElement).toBe(publicView)
      await panel.trigger('keydown', { key: 'End' })
      expect(document.activeElement).toBe(logout)
      await panel.trigger('keydown', { key: 'ArrowDown' })
      expect(document.activeElement).toBe(toggle)
      await panel.trigger('keydown', { key: 'ArrowUp' })
      expect(document.activeElement).toBe(logout)
      await panel.trigger('keydown', { key: 'Home' })
      expect(document.activeElement).toBe(toggle)
      await panel.trigger('keydown', { key: 'a' })
      expect(document.activeElement).toBe(toggle)
      wrapper.unmount()
    })

    it('schließt mit Escape und gibt den Fokus an den Knopf zurück', async () => {
      const { wrapper } = await mountWithUser()
      await button(wrapper).trigger('click')
      await flushPromises()

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await flushPromises()

      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      expect(document.activeElement).toBe(button(wrapper).element)
      wrapper.unmount()
    })

    // pointerdown covers mouse and touch; iOS sends no document click for taps
    // on non-interactive areas.
    it('schließt beim Klick/Tap außerhalb, nicht beim Klick im Panel', async () => {
      const { wrapper } = await mountWithUser()
      const pointerdown = (el: Element) => el.dispatchEvent(new Event('pointerdown', { bubbles: true }))
      await button(wrapper).trigger('click')

      pointerdown(wrapper.find('.user-menu-info').element)
      await flushPromises()
      expect(wrapper.find('.user-menu-panel').exists()).toBe(true)

      pointerdown(document.body)
      await flushPromises()
      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)

      // A click outside while the menu is closed has no effect.
      pointerdown(document.body)
      await flushPromises()
      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      wrapper.unmount()
    })

    it('schließt, wenn der Fokus das Menü verlässt, nicht aber innerhalb davon', async () => {
      const { wrapper } = await mountWithUser()
      await button(wrapper).trigger('click')
      await flushPromises()
      const panel = wrapper.find('.user-menu-panel')

      await panel.find('.user-menu-link').trigger('focusout', { relatedTarget: panel.findAll('.user-menu-link')[1].element })
      expect(wrapper.find('.user-menu-panel').exists()).toBe(true)
      // Focus into nothing (relatedTarget null, e.g. a window switch) does not close
      await panel.find('.user-menu-link').trigger('focusout', { relatedTarget: null })
      expect(wrapper.find('.user-menu-panel').exists()).toBe(true)

      await panel.find('.user-menu-link').trigger('focusout', { relatedTarget: document.body })
      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      wrapper.unmount()
    })

    it('öffnet das Profil und schließt dabei das Menü', async () => {
      const { wrapper, router } = await mountWithUser()
      await button(wrapper).trigger('click')

      await wrapper.find('a.user-menu-link').trigger('click')
      await flushPromises()

      expect(router.currentRoute.value.path).toBe('/admin/profile')
      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      wrapper.unmount()
    })

    it('wechselt über „Öffentliche Ansicht“ auf die Startseite', async () => {
      const { wrapper, router } = await mountWithUser()
      await button(wrapper).trigger('click')

      await wrapper.findAll('button.user-menu-link')[0].trigger('click')
      await flushPromises()

      expect(router.currentRoute.value.path).toBe('/')
      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      wrapper.unmount()
    })

    it('schließt das Menü beim Routenwechsel', async () => {
      const { wrapper, router } = await mountWithUser()
      await button(wrapper).trigger('click')

      await router.push('/admin/tags')
      await flushPromises()

      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      wrapper.unmount()
    })

    it('schließt beim zweiten Klick auf den Knopf', async () => {
      const { wrapper } = await mountWithUser()
      await button(wrapper).trigger('click')
      await button(wrapper).trigger('click')
      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      expect(button(wrapper).attributes('aria-expanded')).toBe('false')
      wrapper.unmount()
    })

    it('entfällt ohne angemeldeten Benutzer', async () => {
      const { wrapper } = await mountLayout()
      expect(wrapper.find('.user-menu').exists()).toBe(false)
      wrapper.unmount()
    })

    it('wechselt per Knopf in der Kopfzeile zwischen hellem und dunklem Modus', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()
      const toggle = wrapper.find('.topbar-right .icon-btn')
      const before = toggle.attributes('aria-label')

      await toggle.trigger('click')

      expect(toggle.attributes('aria-label')).not.toBe(before)
      expect(['Dark Mode aktivieren', 'Light Mode aktivieren']).toContain(before)
      await toggle.trigger('click')
      expect(toggle.attributes('aria-label')).toBe(before)
      wrapper.unmount()
    })

    it('leitet das Kürzel aus der E-Mail ab', async () => {
      useAuth().setAuth('jwt-1', { email: 'user@example.com' })
      const { wrapper } = await mountLayout()
      expect(wrapper.find('.user-avatar').text()).toBe('US')
      wrapper.unmount()
    })
  })

  describe('Mandanten-Umschalter in der Kopfzeile', () => {
    it('sitzt in der Kopfzeile und zeigt die Statusseiten-Namen', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      mockedFetch().mockResolvedValue(asResponse({
        ok: true, status: 200,
        json: async () => ([{ ID: 5, Name: 'Page A' }, { ID: 6, Name: 'Page B' }]),
      }))
      const { wrapper } = await mountLayout()

      const select = wrapper.find('header.admin-topbar .tenant-select')
      expect(select.exists()).toBe(true)
      expect(select.findAll('option').map(o => o.text())).toEqual(['Page A', 'Page B'])
    })

    it('wechselt die aktive Statusseite und lädt die aktuelle Route neu (Overrides)', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      mockedFetch().mockResolvedValue(asResponse({
        ok: true, status: 200,
        json: async () => ([{ ID: 5, Name: 'Page A' }, { ID: 6, Name: 'Page B' }]),
      }))
      const { wrapper, router } = await mountLayout({ initialPath: '/admin/tags' })
      const visited: string[] = []
      router.afterEach((to) => { visited.push(to.path) })

      // setSelected is internal to DOMWrapper's typings but is what selects an <option>.
      await (wrapper.findAll('.tenant-select option')[1] as unknown as { setSelected(): Promise<void> }).setSelected()
      await flushPromises()

      expect(String(useTenant().activeTenantId.value)).toBe('6')
      expect(visited).toEqual(['/', '/admin/tags'])
      expect(router.currentRoute.value.path).toBe('/admin/tags')
    })
  })

  describe('Mobilmenü', () => {
    afterEach(() => {
      document.body.innerHTML = ''
      document.body.style.overflow = ''
    })

    it('listet im geöffneten Menü alle Bereiche mit Überschrift untereinander', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()

      await wrapper.find('.burger-btn').trigger('click')
      expect(wrapper.find('.admin-nav').classes()).toContain('is-open')
      expect(wrapper.findAll('.area-items-label').map(l => l.text())).toEqual(['Betrieb', 'Inhalte', 'Verwaltung', 'System'])
      expect(wrapper.find('.burger-btn').attributes('aria-expanded')).toBe('true')
      expect(wrapper.find('.mobile-overlay').exists()).toBe(true)
    })

    it('legt den Fokus beim Öffnen auf den ersten Eintrag und beim Schließen zurück auf den Burger', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout({ attach: true })
      const burger = wrapper.find('.burger-btn').element

      await wrapper.find('.burger-btn').trigger('click')
      await flushPromises()
      expect(document.activeElement).toBe(wrapper.find('.area-items a').element)

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await flushPromises()
      expect(wrapper.find('.admin-nav').classes()).not.toContain('is-open')
      expect(document.activeElement).toBe(burger)
      wrapper.unmount()
    })

    it('schließt beim Klick auf den Hintergrund', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout()

      await wrapper.find('.burger-btn').trigger('click')
      await wrapper.find('.mobile-overlay').trigger('click')

      expect(wrapper.find('.admin-nav').classes()).not.toContain('is-open')
      expect(wrapper.find('.mobile-overlay').exists()).toBe(false)
    })

    it('schließt zuerst das Benutzermenü, erst ein zweites Escape das Mobilmenü', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
      const { wrapper } = await mountLayout({ attach: true })

      await wrapper.find('.burger-btn').trigger('click')
      await wrapper.find('.user-menu-button').trigger('click')
      await flushPromises()

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await flushPromises()
      expect(wrapper.find('.user-menu-panel').exists()).toBe(false)
      expect(wrapper.find('.admin-nav').classes()).toContain('is-open')

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await flushPromises()
      expect(wrapper.find('.admin-nav').classes()).not.toContain('is-open')
      wrapper.unmount()
    })
  })
  // Access check per tab (route.meta.permission) in the layout.
  describe('Zugriff auf Tabs', () => {
    const asRole = (perms: string, extra: AuthUser = {}) => {
      const pages = [{ ID: 7, Name: 'Acme', RoleName: 'Rolle', Permissions: perms }] as MyPage[]
      useAuth().setAuth('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: '', ...extra })
      useTenant().setMyPages(pages)
      useTenant().setActiveTenant(7)
      global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => pages })
    }
    beforeEach(() => { probeMounted = 0 })

    it('zeigt „Keine Berechtigung“ statt der Seite, wenn das Leserecht fehlt', async () => {
      asRole('notices:read')
      const { wrapper } = await mountLayout({ initialPath: '/admin/components' })

      expect(wrapper.find('.probe').exists()).toBe(false)
      expect(probeMounted).toBe(0)
      expect(wrapper.find('h2').text()).toBe('Keine Berechtigung')
      expect(wrapper.find('[role="status"].sr-only').text()).toBe('Zugriff verweigert (forbidden)')
      expect(wrapper.find('section[data-status="forbidden"]').exists()).toBe(true)
      const link = wrapper.find('.forbidden__link')
      expect(link.text()).toBe('Weiter zu Ankündigungen')
      expect(link.attributes('href')).toBe('/admin/notices')
    })

    it('leitet nicht weiter, die Adresse bleibt stehen', async () => {
      asRole('notices:read')
      const { router } = await mountLayout({ initialPath: '/admin/components' })
      expect(router.currentRoute.value.path).toBe('/admin/components')
    })

    it('baut die Seite ohne Leserecht nicht auf, auch nicht kurz während die Rollen laden', async () => {
      useAuth().setAuth('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: '' })
      useTenant().setMyPages([])
      let release!: (value?: unknown) => void
      const late = new Promise(resolve => { release = resolve })
      global.fetch = vi.fn(async () => {
        await late
        return { ok: true, status: 200, json: async () => [{ ID: 7, Name: 'Acme', Permissions: 'notices:read' }] }
      }) as unknown as typeof fetch
      const router = makeRouter()
      router.push('/admin/components')
      await router.isReady()
      const wrapper = mount(AdminLayout, { global: { plugins: [i18n, router] } })
      await flushPromises()

      // Roles still loading: neither the page nor "no permission", but a loading hint.
      expect(probeMounted).toBe(0)
      expect(wrapper.find('h2').exists()).toBe(false)
      expect(wrapper.find('main [role="status"]').text()).toBe('Rechte werden geladen …')

      release()
      await flushPromises()
      expect(probeMounted).toBe(0)
      expect(wrapper.find('h2').text()).toBe('Keine Berechtigung')
    })

    it('zeigt die Seite mit Leserecht, auch wenn nur Anlegen vergeben ist (create impliziert read)', async () => {
      asRole('components:create')
      const { wrapper } = await mountLayout({ initialPath: '/admin/components' })
      expect(wrapper.find('.probe').exists()).toBe(true)
      expect(wrapper.find('section[data-status="forbidden"]').exists()).toBe(false)
    })

    it('Global Admin sieht jede Seite ohne Rollen-Abfrage', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const { wrapper } = await mountLayout({ initialPath: '/admin/users' })
      expect(wrapper.find('.probe').exists()).toBe(true)
    })

    it('globale Tabs sind auch mit dem Rechte-String in der Seitenrolle gesperrt', async () => {
      asRole('users:update,roles:read,audit:read,tags:delete')
      for (const path of ['/admin/users', '/admin/roles', '/admin/audit', '/admin/tags']) {
        const { wrapper } = await mountLayout({ initialPath: path })
        expect(wrapper.find('h2').text()).toBe('Keine Berechtigung')
        wrapper.unmount()
      }
    })

    it('Statusseiten-Tab (plattformweite Liste) ist nur für Global Admins, auch mit pages:update', async () => {
      asRole('pages:create,pages:update,pages:delete')
      const { wrapper } = await mountLayout({ initialPath: '/admin/pages' })
      expect(wrapper.find('h2').text()).toBe('Keine Berechtigung')
      expect(wrapper.findAll('.area-items a').map(a => a.text())).not.toContain('Statusseiten')
    })

    it('ohne jede Rolle erklärt die Seite, dass noch kein Zugriff vergeben ist', async () => {
      useAuth().setAuth('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: '' })
      useTenant().setMyPages([])
      global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] })
      const { wrapper } = await mountLayout({ initialPath: '/admin/components' })
      expect(wrapper.find('h2').text()).toBe('Keine Berechtigung')
      expect(wrapper.find('.forbidden__text').text()).toContain('noch keine Rolle')
      expect(wrapper.find('.forbidden__link').exists()).toBe(false)
    })

    // Status page only: a role without any admin right (empty, or only
    // admin_dashboard) has no admin area; its members land on their status page.
    describe('Rolle „Nur Statusseite“', () => {
      const asPageOnly = (pages: MyPage[]) => {
        useAuth().setAuth('jwt-1', { email: 'user@x.de', is_global_admin: false, permissions: '' })
        useTenant().setMyPages(pages)
        useTenant().setActiveTenant(pages[0].ID)
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => pages })
      }

      for (const perms of ['', 'admin_dashboard']) {
        it(`leitet Tabs und Startseite zur Statusseite um (Rechte: "${perms}")`, async () => {
          for (const path of ['/admin', '/admin/components', '/admin/incidents']) {
            asPageOnly([{ ID: 7, Name: 'Acme', Slug: 'acme', Permissions: perms }] as MyPage[])
            const { wrapper, router } = await mountLayout({ initialPath: path })
            await flushPromises()
            expect(router.currentRoute.value.path, path).toBe('/status/acme')
            expect(probeMounted).toBe(0)
            expect(wrapper.find('section[data-status="forbidden"]').exists()).toBe(false)
            wrapper.unmount()
          }
        })
      }

      it('uses the first status page when the active page is not among the memberships', async () => {
        asPageOnly([{ ID: 7, Name: 'Acme', Slug: 'acme', Permissions: '' }, { ID: 9, Name: 'Beta', Slug: 'beta', Permissions: '' }] as MyPage[])
        useTenant().setActiveTenant(42)
        const { router } = await mountLayout({ initialPath: '/admin/components' })
        await flushPromises()
        expect(router.currentRoute.value.path).toBe('/status/acme')
      })

      it('treats an empty pages answer as no membership', async () => {
        asPageOnly([{ ID: 7, Name: 'Acme', Slug: 'acme', Permissions: '' }] as MyPage[])
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => null })
        const { wrapper, router } = await mountLayout({ initialPath: '/admin/components' })
        await flushPromises()
        expect(router.currentRoute.value.path).toBe('/admin/components')
        expect(wrapper.find('section[data-status="forbidden"]').exists()).toBe(true)
      })

      it('lässt das eigene Profil erreichbar', async () => {
        asPageOnly([{ ID: 7, Name: 'Acme', Slug: 'acme', Permissions: '' }] as MyPage[])
        const { router } = await mountLayout({ initialPath: '/admin/profile' })
        await flushPromises()
        expect(router.currentRoute.value.path).toBe('/admin/profile')
      })

      it('bietet in der Seitenwahl nur Seiten mit Admin-Recht und wechselt weg von einer reinen Statusseite', async () => {
        const pages = [
          { ID: 8, Name: 'Nur lesen', Slug: 'lesen', Permissions: '' },
          { ID: 7, Name: 'Acme', Slug: 'acme', Permissions: 'components:read' },
        ]
        asPageOnly(pages)
        const { wrapper, router } = await mountLayout({ initialPath: '/admin/components' })
        await flushPromises()
        expect(String(useTenant().activeTenantId.value)).toBe('7')
        expect(router.currentRoute.value.path).toBe('/admin/components')
        expect(wrapper.find('.probe').exists()).toBe(true)
        expect(wrapper.findAll('#tenant-select option').map(o => o.text())).toEqual(['Acme'])
      })
    })

    it('Startseite /admin führt zum ersten sichtbaren Tab', async () => {
      asRole('subscribers:read,notices:read')
      const { router } = await mountLayout({ initialPath: '/admin' })
      expect(router.currentRoute.value.path).toBe('/admin/notices')
    })

    // A role with only admin_dashboard is "status page only" (see above).
    it('Startseite führt Global Admins zur Übersicht', async () => {
      useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true, permissions: '' })
      const admin = await mountLayout({ initialPath: '/admin' })
      expect(admin.router.currentRoute.value.path).toBe('/admin/overview')
    })

    it('hat in „Keine Berechtigung“ keine axe-Funde', async () => {
      asRole('notices:read')
      const { wrapper } = await mountLayout({ initialPath: '/admin/components', attach: true })
      expect(await axe(wrapper)).toHaveNoViolations()
      wrapper.unmount()
    })
  })
})
