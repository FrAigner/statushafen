import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, DOMWrapper } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { useCurrentPageSlug } from '../../composables/useCurrentPageSlug'
import { usePublicPageLanguages } from '../../composables/usePublicPageLanguages'
import { useTheme } from '../../composables/useTheme'
import StatusPage from './StatusPage.vue'
import { BRAND_SECONDARY_BACKGROUNDS } from '../../utils/brandContrast'
import { contrastRatio, parseHex } from '../../utils/contrast'
import { axe } from '../../test-support/axe.js'

let openEditAboMock = vi.fn()
let refreshMock = vi.fn()

const SubscriptionManagerStub = {
  props: ['isLoggedIn', 'currentUser', 'components', 'slug'],
  emits: ['open-wizard', 'subscription-state'],
  setup(_props, { expose }) {
    expose({ openEditAbo: openEditAboMock, refresh: (...a) => refreshMock(...a) })
  },
  template: '<div class="subscription-manager-stub" />',
}

function dashboardFixture(overrides = {}) {
  return {
    page: { ID: 1, Name: 'Acme', PrimaryColor: '', SecondaryColor: '', HeaderStyle: 'classic' },
    components: [
      { ID: 10, Name: 'API', Status: 'operational', Tags: [{ Name: 'DE' }], Children: [] },
      {
        ID: 11, Name: 'Web', Status: 'degraded_performance', Tags: [{ Name: 'US' }],
        Children: [{ ID: 12, Name: 'CDN', Status: 'operational', Tags: [{ Name: 'US' }], Children: [] }],
      },
    ],
    incidents: [
      {
        ID: 100, Title: 'API down', Message: 'API antwortet nicht', Status: 'investigating',
        IsMaintenance: false, Tags: [], Components: [{ Name: 'API' }], CreatedAt: '2026-07-01T10:00:00Z',
      },
      {
        ID: 101, Title: 'Geplantes Upgrade', Message: 'Wartung', Status: 'scheduled', IsMaintenance: true,
        ScheduledStartAt: '2026-08-01T10:00:00Z', ScheduledEndAt: '2026-08-01T12:00:00Z', Components: [],
      },
    ],
    notices: [],
    ...overrides,
  }
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/status/:slug', component: { template: '<div/>' } }, { path: '/login', component: { template: '<div/>' } }],
  })
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// Antwortet je nach URL, damit Reihenfolge und Anzahl der Requests (Dashboard,
// Verfügbarkeit, Updates der aktuellen Vorfälle, Verlauf-Dialog) keine Rolle spielen.
// availability: null => 404 (Balken entfallen); detail: Antwort für /incidents/:id.
function routedFetch({ dashboard = dashboardFixture(), availability = null, detail = { updates: [] }, detailError = null } = {}) {
  return vi.fn(async (url) => {
    if (url.includes('/availability')) return availability ? jsonResponse(availability) : jsonResponse({}, 404)
    if (url.includes('/update-translations')) return jsonResponse({ updates: {} })
    if (url.includes('/incidents/')) {
      if (detailError) throw detailError
      return jsonResponse(detail)
    }
    return jsonResponse(dashboard)
  })
}
const dashboardCalls = (fetchMock) => fetchMock.mock.calls.filter(([url]) => url.includes('/dashboard'))

// Abschnitte der Seite (4a) per Überschrift statt per CSS-Klasse.
const section = (w, id) => w.find(`section[aria-labelledby="${id}"]`)
const currentSection = (w) => section(w, 'sp-current-heading')
const currentCards = (w) => currentSection(w).findAll('.sp-incident')
const maintenanceCards = (w) => section(w, 'sp-maintenance-heading').findAll('.sp-incident')

let activeWrapper = null

async function mountStatusPage({ slug = 'acme', routePath = '/status/acme', fetchImpl } = {}) {
  const router = makeRouter()
  router.push(routePath)
  await router.isReady()

  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse(dashboardFixture()))

  const wrapper = mount(StatusPage, {
    props: { slug },
    global: {
      plugins: [i18n, router],
      stubs: {
        ComponentTreeNode: { props: ['node', 'depth'], template: '<div class="component-tree-node-stub">{{ node.Name }}</div>' },
        SubscribeWizard: { props: ['components', 'slug'], emits: ['close', 'subscribed'], template: '<div class="subscribe-wizard-stub" />' },
        SubscriptionManager: SubscriptionManagerStub,
      },
    },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper, router }
}

describe('StatusPage', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    openEditAboMock = vi.fn()
    refreshMock = vi.fn()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // Tests assertieren auf den deutschen Wortlaut übersetzter Texte;
    // ohne gespeicherte Präferenz würde detectInitialLocale() in der Testumgebung
    // (happy-dom navigator.language === 'en-US') sonst auf 'en' fallen.
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('lädt die Dashboard-Daten beim Mounten und zeigt Seitenname und Komponenten', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(dashboardFixture()))
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    expect(fetchMock).toHaveBeenCalledWith('/api/public/pages/acme/dashboard', expect.anything())
    expect(wrapper.find('.sp-brand-name').text()).toBe('Acme')
    expect(wrapper.findAll('.component-tree-node-stub')).toHaveLength(2)
  })

  // Sprachen der Seite für den Umschalter im Kopf.
  it('gibt die Sprachen der Seite aus dem Dashboard an den Umschalter und leert sie beim Verlassen', async () => {
    const languages = [{ code: 'de', name: 'Deutsch', is_default: true }, { code: 'hr', name: 'Hrvatski', is_default: false }]
    const { pageLanguages } = usePublicPageLanguages()
    const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ dashboard: dashboardFixture({ languages }) }) })
    expect(pageLanguages.value).toEqual(languages)
    wrapper.unmount()
    activeWrapper = null
    expect(pageLanguages.value).toEqual([])
  })

  describe('Seiten-Layout', () => {
    const withLayout = (PageLayout) => dashboardFixture({
      page: { ID: 1, Name: 'Acme', PageLayout, HeaderStyle: 'centered', PrimaryColor: '#123456' },
      notices: [{ ID: 5, Title: 'Hinweis', Message: 'x', Level: 'info', Collapsed: false, CreatedAt: '2026-07-01T10:00:00Z' }],
    })
    const mountWith = (layout) => mountStatusPage({
      fetchImpl: vi.fn().mockResolvedValue(jsonResponse(withLayout(layout))),
    })

    it.each([undefined, '', 'standard', 'unbekannt'])('rendert bei %j das bisherige Layout', async (layout) => {
      const { wrapper } = await mountWith(layout)
      expect(wrapper.find('.pl-root').exists()).toBe(false)
      expect(wrapper.find('.component-tree').exists()).toBe(true)
      expect(currentSection(wrapper).exists()).toBe(true)
    })

    it('rendert bei liste Gesamtstatus oben, Komponentenkarten mit 90-Tage-Balken und Zeitleiste', async () => {
      const days = Array.from({ length: 90 }, (_, i) => ({ date: `2026-07-${String((i % 28) + 1).padStart(2, '0')}`, status: 'operational', availability: 100, downtime_seconds: 0 }))
      const availability = {
        uptime_percent: 99.9,
        days,
        components: [{ id: 10, name: 'API', uptime_percent: 100, days }, { id: 12, name: 'CDN', uptime_percent: 100, days }],
      }
      const fetchMock = routedFetch({ dashboard: withLayout('liste'), availability })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(wrapper.find('.component-tree').exists()).toBe(false)
      expect(wrapper.find('.sp-hero').exists()).toBe(true)
      expect(wrapper.find('.pl-overall').exists()).toBe(false)
      expect(wrapper.findAll('.pl-card')).toHaveLength(2)
      // Gesamtbalken + je ein Balken für die beiden Blätter (Gruppe „Web“ hat keinen).
      expect(wrapper.findAll('.avail__day')).toHaveLength(90 * 3)
      expect(wrapper.find('.pl-timeline').text()).toContain('API down')
      expect(fetchMock).toHaveBeenCalledWith('/api/public/pages/acme/availability?include=components', expect.anything())
    })

    it.each(['standard', 'liste'])('behält Header, Branding, Hinweise, Abonnieren und Suche bei (%s)', async (layout) => {
      const { wrapper } = await mountWith(layout)
      expect(wrapper.find('.sp-brand-name').text()).toBe('Acme')
      expect(wrapper.find('.status-header--centered').exists()).toBe(true)
      expect(wrapper.find('.content-area').attributes('style')).toContain('--primary: #123456')
      expect(wrapper.find('.notice-card').text()).toContain('Hinweis')
      expect(wrapper.text()).toContain('Geplante Wartung')
      expect(wrapper.find('.subscription-manager-stub').exists()).toBe(true)
      await wrapper.find('.btn-subscribe-cta').trigger('click')
      expect(wrapper.find('.subscribe-wizard-stub').exists()).toBe(true)
    })

    it('öffnet den Verlauf-Dialog aus der Zeitleiste von liste', async () => {
      const detail = { updates: [{ id: 1, status: 'investigating', message: 'Wir prüfen', created_at: '2026-07-01T10:00:00Z' }] }
      const fetchMock = vi.fn()
        .mockResolvedValueOnce(jsonResponse(withLayout('liste')))
        .mockResolvedValue(jsonResponse(detail))
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      await wrapper.find('.pl-link').trigger('click')
      await flushPromises()
      expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents/100', expect.anything())
      expect(document.body.textContent).toContain('Wir prüfen')
    })

    it('zeigt in liste trotz Filter den echten Gesamtstatus (Review-Befund 2)', async () => {
      const fixture = withLayout('liste')
      fixture.components[0].Status = 'major_outage'
      const { wrapper } = await mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fixture)) })
      await wrapper.findAll('.filter-tag.status-tag-degraded_performance')[0].trigger('click')
      expect(wrapper.find('.sp-hero-title').text()).toBe('Größere Störung')
      await wrapper.findAll('.filter-tag.status-tag-operational')[0].trigger('click')
      expect(wrapper.find('.sp-hero-title').text()).toBe('Größere Störung')
    })

    it('filtert in liste die Komponenten nach Status', async () => {
      const { wrapper } = await mountWith('liste')
      await wrapper.findAll('.filter-tag.status-tag-degraded_performance')[0].trigger('click')
      expect(wrapper.findAll('.pl-card')).toHaveLength(1)
      expect(wrapper.find('.pl-card').text()).toContain('Web')
    })
  })

  it('reicht ein Subscriber-Token aus der Query an den Dashboard-Request weiter', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(dashboardFixture()))
    await mountStatusPage({ routePath: '/status/acme?token=tok123', fetchImpl: fetchMock })

    expect(fetchMock).toHaveBeenCalledWith('/api/public/pages/acme/dashboard?token=tok123', expect.anything())
  })

  // Regressionstest Zustand D: der Subscriber-Token im Link
  // gewährt nur Lesezugriff auf eine private Seite (useAuth bleibt
  // ausgeloggt) - die neue Abo-Zustandslogik (A/B/C) darf diesen Zugriffsweg
  // nicht brechen; die Seite lädt normal und zeigt wie bei Zustand A nur
  // "Jetzt abonnieren", ohne Verwalten-Knopf oder Fehlermeldung.
  it('Zustand D (Token-Link): Seite bleibt funktional, Abo-Zustandslogik bricht den Zugriff nicht', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(dashboardFixture()))
    const { wrapper } = await mountStatusPage({ routePath: '/status/acme?token=tok123', fetchImpl: fetchMock })

    expect(wrapper.find('.sp-brand-name').text()).toBe('Acme')
    expect(wrapper.find('.btn-subscribe-cta').text()).toContain('Jetzt abonnieren')
    expect(wrapper.find('.btn-subscribe-cta--outline').exists()).toBe(false)
    expect(wrapper.find('.subscribe-no-sub').exists()).toBe(false)
  })

  it('zeigt den privaten Fehlerzustand mit Login-Link bei 401', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}, 401))
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    expect(wrapper.find('.private-error-state').exists()).toBe(true)
    expect(wrapper.text()).toContain('Statusseite nicht verfügbar')
    expect(wrapper.find('a[href^="/login"]').exists()).toBe(true)
  })

  // Eine private Seite ohne Berechtigung antwortet wie eine fehlende Seite mit 404.
  // Der Zustand muss beides abdecken und den Weg zur Anmeldung offen lassen.
  it('zeigt bei 404 denselben Zustand mit Login-Link und neutralem Text', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ code: 'page_not_found' }, 404))
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    expect(wrapper.find('.private-error-state').exists()).toBe(true)
    expect(wrapper.text()).toContain('Statusseite nicht verfügbar')
    expect(wrapper.text()).toContain('gibt es nicht oder ist privat')
    expect(wrapper.text()).not.toContain('Daten konnten nicht geladen werden')
    const loginLink = wrapper.find('.private-error-state a[href^="/login"]').attributes('href')
    expect(new URLSearchParams(loginLink.split('?')[1]).get('page')).toBe('acme')
  })

  it('bietet bei 404 den SSO-Einstieg über den Slug an, wenn die Auskunft ebenfalls 404 liefert', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ code: 'page_not_found' }, 404))
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    expect(wrapper.find('.private-error-state a.oidc-login-btn').attributes('href')).toBe('/api/auth/sso/login?slug=acme')
  })

  // SSO-Knopf der Seite direkt im privaten Fehlerzustand, und der
  // Login-Link gibt die Seite weiter, damit die Login-Seite ihn ebenfalls zeigt.
  it('bietet im privaten Fehlerzustand das Seiten-SSO an', async () => {
    const fetchMock = vi.fn(async (url) => (url.endsWith('/sso')
      ? jsonResponse({ oidc_enabled: true, page_id: 1 })
      : jsonResponse({}, 403)))
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    expect(wrapper.find('.private-error-state a.oidc-login-btn').attributes('href')).toBe('/api/auth/oidc/login?page=1')
    const loginLink = wrapper.find('.private-error-state a[href^="/login"]').attributes('href')
    expect(new URLSearchParams(loginLink.split('?')[1]).get('page')).toBe('acme')
  })

  it('merkt sich den Slug der angezeigten Seite für den Login-Knopf im Kopf', async () => {
    const { wrapper } = await mountStatusPage()
    expect(useCurrentPageSlug().currentPageSlug.value).toBe('acme')

    await wrapper.setProps({ slug: 'beta' })
    expect(useCurrentPageSlug().currentPageSlug.value).toBe('beta')

    wrapper.unmount()
    activeWrapper = null
    expect(useCurrentPageSlug().currentPageSlug.value).toBe(null)
  })

  it('zeigt den privaten Fehlerzustand bei 403 (Zugriff verweigert)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}, 403))
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    expect(wrapper.find('.private-error-state').exists()).toBe(true)
  })

  it('zeigt eine generische Fehlermeldung bei sonstigen Serverfehlern', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}, 500))
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    expect(wrapper.find('.private-error-state').exists()).toBe(false)
    expect(wrapper.text()).toContain('Daten konnten nicht geladen werden.')
  })

  it('extrahiert verfügbare Regionen rekursiv aus den Component-Tags', async () => {
    const { wrapper } = await mountStatusPage()

    const regionButtons = wrapper.findAll('.tag-group')[0].findAll('.filter-tag').map(b => b.text())
    expect(regionButtons).toEqual(['Alle', 'DE', 'US'])
  })

  describe('Branding-Kontrast', () => {
    const { theme } = useTheme()
    afterEach(() => { theme.value = 'light' })

    const mountBranded = (page) => mountStatusPage({
      fetchImpl: vi.fn().mockResolvedValue(jsonResponse(dashboardFixture({
        page: { ID: 1, Name: 'Acme', HeaderStyle: 'classic', ...page },
      }))),
    })

    it('dunkelt eine zu helle Primärfarbe für weiße Schrift ab und lässt eine passende Sekundärfarbe', async () => {
      const { wrapper } = await mountBranded({ PrimaryColor: '#4c6ef5', SecondaryColor: '#5e6c84' })
      const style = wrapper.find('.content-area').attributes('style')
      expect(style).toContain('--primary: #4869e9')
      expect(style).toContain('--primary-hover: #4869e9')
      expect(style).toContain('--text-secondary: #5e6c84')
    })

    it('hellt die Sekundärfarbe im Dunkelmodus auf und folgt dem Themewechsel', async () => {
      theme.value = 'dark'
      const { wrapper } = await mountBranded({ PrimaryColor: '', SecondaryColor: '#5e6c84' })
      // #7e899d erreichte auf der Hero-Tönung (--ok-soft dunkel, #132a1e) nur 4,32:1.
      const dark = wrapper.find('.content-area').attributes('style')
      expect(dark).toBe('--text-secondary: #8691a3;')
      for (const bg of BRAND_SECONDARY_BACKGROUNDS.dark) {
        expect(contrastRatio(parseHex('#8691a3'), parseHex(bg))).toBeGreaterThanOrEqual(4.5)
      }
      expect(contrastRatio(parseHex('#7e899d'), parseHex('#132a1e'))).toBeLessThan(4.5)

      theme.value = 'light'
      await flushPromises()
      expect(wrapper.find('.content-area').attributes('style')).toBe('--text-secondary: #5e6c84;')
    })

    it('prüft hell gegen Panel und Hover-Fläche, nicht nur gegen Weiß', async () => {
      // #6b778c hält auf Weiß 4,5:1, auf #f0f1f3 nicht.
      const { wrapper } = await mountBranded({ SecondaryColor: '#6b778c' })
      expect(wrapper.find('.content-area').attributes('style')).toBe('--text-secondary: #606b7e;')
    })
  })

  describe('Filterleiste', () => {
    const manyTags = (n) => Array.from({ length: n }, (_, i) => ({ Name: `R${String(i).padStart(2, '0')}` }))
    const fixtureWithTags = (n) => dashboardFixture({
      components: [{ ID: 10, Name: 'API', Status: 'operational', Tags: manyTags(n), Children: [] },
        { ID: 11, Name: 'Web', Status: 'operational', Tags: [{ Name: 'ZZ' }], Children: [] }],
    })
    const mountMany = (n) => mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fixtureWithTags(n))) })
    const regionChips = (wrapper) => wrapper.findAll('.tag-group')[0].findAll('.filter-tag').map(b => b.text())
    const css = () => readFileSync(resolve(__dirname, '../../style.css'), 'utf-8')

    it('Suchfeld belegt die volle Zeilenbreite und zeigt den Platzhalter vollständig', async () => {
      const { wrapper } = await mountStatusPage()
      expect(wrapper.find('.search-input').attributes('placeholder')).toBe('Komponenten oder Vorfälle suchen...')
      const rule = css().match(/\.filter-bar\s+\.search-wrapper\s*\{([^}]*)\}/)
      expect(rule).not.toBeNull()
      expect(rule[1]).toContain('flex: 1 1 100%')
    })

    it('baut Region- und Status-Zeile als Raster mit fester Label-Spalte und gemeinsamem Chip-Container', async () => {
      const { wrapper } = await mountStatusPage()
      const groups = wrapper.findAll('.tag-group')
      expect(groups).toHaveLength(2)
      for (const g of groups) {
        expect(g.find('.tag-label').exists()).toBe(true)
        expect(g.find('.tag-chips').exists()).toBe(true)
        expect(g.find('.tag-chips').findAll('.filter-tag').length).toBeGreaterThan(0)
      }
      const rule = css().match(/\.tag-group\s*\{([^}]*)\}/)
      expect(rule).not.toBeNull()
      expect(rule[1]).toContain('display: grid')
      expect(rule[1]).toMatch(/grid-template-columns:\s*var\(--filter-label-width\)\s+1fr/)
    })

    it('zeigt bis zur Schwelle (8 Einträge inkl. „Alle“) alle Chips ohne Mehr-anzeigen-Knopf', async () => {
      const { wrapper } = await mountMany(7) // 7 + ZZ + Alle = 9 -> über Schwelle
      expect(wrapper.find('.tag-more').exists()).toBe(true)
      wrapper.unmount()
      const { wrapper: w2 } = await mountMany(6) // 6 + ZZ + Alle = 8 -> an der Schwelle
      expect(w2.find('.tag-more').exists()).toBe(false)
      expect(regionChips(w2)).toHaveLength(8)
    })

    it('klappt über der Schwelle ein, zeigt „Mehr anzeigen (n)“ und klappt per Knopf auf und zu', async () => {
      const { wrapper } = await mountMany(12) // 13 Regionen
      expect(regionChips(wrapper)).toEqual(['Alle', 'R00', 'R01', 'R02', 'R03', 'R04', 'R05'])
      const more = wrapper.find('.tag-more')
      expect(more.text()).toBe('Mehr anzeigen (7)')
      expect(more.attributes('aria-expanded')).toBe('false')

      await more.trigger('click')
      expect(regionChips(wrapper)).toHaveLength(14)
      expect(wrapper.find('.tag-more').text()).toBe('Weniger anzeigen')
      expect(wrapper.find('.tag-more').attributes('aria-expanded')).toBe('true')

      await wrapper.find('.tag-more').trigger('click')
      expect(regionChips(wrapper)).toHaveLength(7)
      expect(wrapper.find('.tag-more').attributes('aria-expanded')).toBe('false')
    })

    it('hält den aktiven Filter auch eingeklappt sichtbar', async () => {
      const { wrapper } = await mountMany(12)
      await wrapper.find('.tag-more').trigger('click')
      await wrapper.findAll('.filter-tag').find(b => b.text() === 'ZZ').trigger('click')
      await wrapper.find('.tag-more').trigger('click')
      expect(regionChips(wrapper)).toEqual(['Alle', 'R00', 'R01', 'R02', 'R03', 'R04', 'R05', 'ZZ'])
      expect(wrapper.findAll('.tag-group')[0].find('.filter-tag.active').text()).toBe('ZZ')
      expect(wrapper.find('.tag-more').text()).toBe('Mehr anzeigen (6)')
    })

    it('hält mehrere aktive Regionen auch eingeklappt sichtbar und blendet abgewählte wieder aus', async () => {
      const { wrapper } = await mountMany(12)
      await wrapper.find('.tag-more').trigger('click')
      const chip = (name) => wrapper.findAll('.filter-tag').find(b => b.text() === name)
      await chip('R08').trigger('click')
      await chip('R10').trigger('click')
      await wrapper.find('.tag-more').trigger('click')

      expect(regionChips(wrapper)).toEqual(['Alle', 'R00', 'R01', 'R02', 'R03', 'R04', 'R05', 'R08', 'R10'])
      expect(chip('R08').attributes('aria-pressed')).toBe('true')
      expect(chip('R10').attributes('aria-pressed')).toBe('true')
      expect(wrapper.find('.tag-more').text()).toBe('Mehr anzeigen (5)')

      await chip('R08').trigger('click')
      expect(regionChips(wrapper)).toEqual(['Alle', 'R00', 'R01', 'R02', 'R03', 'R04', 'R05', 'R10'])
    })

    it('filtert wie bisher nach Region und setzt über „Alle“ und „Filter zurücksetzen“ zurück', async () => {
      const { wrapper } = await mountMany(12)
      await wrapper.findAll('.filter-tag').find(b => b.text() === 'R01').trigger('click')
      expect(wrapper.text()).toContain('API')
      expect(wrapper.find('.filter-result-count').exists()).toBe(true)
      await wrapper.findAll('.filter-tag').find(b => b.text() === 'Alle').trigger('click')
      expect(wrapper.find('.filter-result-count').exists()).toBe(false)
      await wrapper.findAll('.filter-tag').find(b => b.text() === 'R01').trigger('click')
      await wrapper.find('.clear-all-btn').trigger('click')
      expect(wrapper.find('.filter-result-count').exists()).toBe(false)
      expect(wrapper.findAll('.tag-group')[0].find('.filter-tag.active').text()).toBe('Alle')
    })

    it('hat die neuen Texte in allen Sprachdateien', () => {
      const dir = resolve(__dirname, '../../i18n/locales')
      for (const f of readdirSync(dir).filter(n => n.endsWith('.json'))) {
        const sp = JSON.parse(readFileSync(resolve(dir, f), 'utf-8')).statusPage
        expect(sp.showMore, f).toContain('{count}')
        expect(typeof sp.showLess, f).toBe('string')
      }
    })
  })

  it('gruppiert geplante Wartungsfenster separat von den aktuellen Vorfällen', async () => {
    const { wrapper } = await mountStatusPage()

    expect(wrapper.text()).toContain('Geplante Wartungsarbeiten')
    expect(wrapper.text()).toContain('Geplantes Upgrade')
    // Der Maintenance-Incident darf nicht zusätzlich in "Aktuelle Vorfälle" auftauchen
    const currentIncidentCards = currentCards(wrapper)
    expect(currentIncidentCards).toHaveLength(1)
    expect(currentIncidentCards[0].text()).toContain('API down')
  })

  it('filtert Komponenten und Vorfälle nach Freitext-Suche (debounced)', async () => {
    vi.useFakeTimers()
    const { wrapper } = await mountStatusPage()

    await wrapper.find('.search-input').setValue('API')
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.findAll('.component-tree-node-stub')).toHaveLength(1)
    expect(wrapper.findAll('.component-tree-node-stub')[0].text()).toBe('API')
  })

  describe('Status-Filter auf Vorfälle', () => {
    // Die Kopien in Components tragen absichtlich den manuell gesetzten Status
    // 'operational': maßgeblich ist der (effektive) Status aus dem Baum.
    const comp = (ID) => ({ ID, Name: `C${ID}`, Status: 'operational' })
    const inc = (ID, Status, ids, extra = {}) => ({
      ID, Title: `Vorfall ${ID}`, Message: 'm', Status, IsMaintenance: false,
      Tags: [], Components: ids.map(comp), CreatedAt: '2026-07-01T10:00:00Z', ...extra,
    })
    const fixture = () => dashboardFixture({
      components: [
        { ID: 10, Name: 'API', Status: 'operational', Tags: [{ Name: 'DE' }], Children: [] },
        {
          ID: 11, Name: 'Web', Status: 'degraded_performance', Tags: [{ Name: 'US' }],
          Children: [{ ID: 12, Name: 'CDN', Status: 'partial_outage', Tags: [{ Name: 'US' }], Children: [] }],
        },
        { ID: 13, Name: 'Edge', Status: 'major_outage', Tags: [{ Name: 'FR' }], Children: [] },
      ],
      incidents: [
        inc(1, 'investigating', [11], { Tags: [{ Name: 'US' }] }),
        inc(2, 'identified', [12]),
        inc(3, 'monitoring', [13]),
        inc(4, 'resolved', [10]),
        inc(5, 'investigating', []),
        inc(6, 'identified', [10, 13]),
        inc(7, 'in_progress', [11], { IsMaintenance: true, ScheduledStartAt: '2026-07-01T10:00:00Z' }),
      ],
    })
    const mountFilter = () => mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fixture())) })
    const ids = (w) => w.vm.filteredIncidents.map(i => i.ID)

    it.each([
      [['operational'], [4, 6]],
      [['degraded_performance'], [1, 7]],
      [['partial_outage'], [2]],
      [['major_outage'], [3, 6]],
      [['degraded_performance', 'partial_outage'], [1, 2, 7]],
      [['operational', 'major_outage'], [3, 4, 6]],
    ])('zeigt bei %j die Vorfälle der Komponenten mit diesem Status', async (chips, expected) => {
      const { wrapper } = await mountFilter()
      for (const key of chips) await wrapper.find(`.filter-tag.status-tag-${key}`).trigger('click')
      expect(ids(wrapper)).toEqual(expected)
    })

    it('verträgt Komponenten ohne Children und Vorfälle ohne Components', async () => {
      const fx = dashboardFixture({
        components: [{ ID: 10, Name: 'API', Status: 'major_outage', Tags: [] }],
        incidents: [inc(1, 'investigating', [10]), { ...inc(2, 'investigating', []), Components: undefined }],
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fx)) })
      await wrapper.find('.filter-tag.status-tag-major_outage').trigger('click')
      expect(ids(wrapper)).toEqual([1])
    })

    it('zeigt ohne Status-Filter alle Vorfälle außer geplanten Wartungen', async () => {
      const { wrapper } = await mountFilter()
      expect(ids(wrapper)).toEqual([1, 2, 3, 4, 5, 6, 7])
    })

    it('verknüpft Status- und Regionsfilter bei Vorfällen mit UND', async () => {
      const { wrapper } = await mountFilter()
      await wrapper.find('.filter-tag.status-tag-degraded_performance').trigger('click')
      await wrapper.findAll('.tag-group')[0].findAll('.filter-tag').find(b => b.text() === 'US').trigger('click')
      expect(ids(wrapper)).toEqual([1])
    })

    it('zeigt den passenden Vorfall unter „Aktuelle Vorfälle“ statt des Leerzustands (Fehlerbild: Vorfall fehlte trotz betroffener Komponente)', async () => {
      // dashboardFixture: „API down“ betrifft API; ist API beeinträchtigt,
      // muss der Vorfall beim Chip „Beeinträchtigt“ sichtbar bleiben.
      const fx = dashboardFixture()
      fx.components[0].Status = 'degraded_performance'
      fx.incidents[0].Components = [{ ID: 10, Name: 'API', Status: 'operational' }]
      const { wrapper: w } = await mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fx)) })
      await w.find('.filter-tag.status-tag-degraded_performance').trigger('click')
      const current = currentSection(w)
      expect(current.findAll('.sp-incident').map(c => c.find('h3').text())).toEqual(['API down'])
      expect(current.text()).not.toContain('Keine Vorfälle für den aktiven Filter')
      expect(w.find('.filter-result-count').text()).toContain('2 Komponente(n) · 1 Vorfall/Vorfälle')
    })

    it('blendet Vorfälle anderer Komponenten aus und zeigt dann den Leerzustand', async () => {
      const { wrapper } = await mountStatusPage()
      await wrapper.find('.filter-tag.status-tag-major_outage').trigger('click')
      const current = currentSection(wrapper)
      expect(current.findAll('.sp-incident')).toHaveLength(0)
      expect(current.text()).toContain('Keine Vorfälle für den aktiven Filter')
    })

    it('filtert auch die Zeitleiste im Layout liste nach Komponenten-Status', async () => {
      const fx = fixture()
      fx.page = { ...fx.page, PageLayout: 'liste' }
      const { wrapper } = await mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fx)) })
      await wrapper.find('.filter-tag.status-tag-partial_outage').trigger('click')
      const timeline = wrapper.find('.pl-timeline').text()
      expect(timeline).toContain('Vorfall 2')
      expect(timeline).not.toContain('Vorfall 1')
    })
  })

  describe('Aktuelle Vorfälle und Verlauf', () => {
    const inc = (ID, Status, extra = {}) => ({
      ID, Title: `Vorfall ${ID}`, Message: 'm', Status, IsMaintenance: false,
      Tags: [], Components: [{ ID: 10, Name: 'API' }], CreatedAt: '2026-07-01T10:00:00Z', ...extra,
    })
    const maint = (ID, Status) => inc(ID, Status, {
      IsMaintenance: true, ScheduledStartAt: '2026-07-02T10:00:00Z', ScheduledEndAt: '2026-07-02T12:00:00Z',
    })
    const mountWith = (incidents, page) => mountStatusPage({
      fetchImpl: vi.fn().mockResolvedValue(jsonResponse(dashboardFixture({ incidents, ...(page ? { page } : {}) }))),
    })
    const titles = (list) => list.map(e => e.find('h3, .past-incident-title').text().trim())
    const currentTitles = (w) => (currentSection(w).exists() ? titles(currentCards(w)) : [])
    const pastTitles = (w) => titles(w.findAll('.past-incident'))
    const upcomingTitles = (w) => (section(w, 'sp-maintenance-heading').exists() ? titles(maintenanceCards(w)) : [])

    it.each([
      ['investigating', 'Vorfall', 'current'],
      ['identified', 'Vorfall', 'current'],
      ['monitoring', 'Vorfall', 'current'],
      ['foo_bar', 'Vorfall', 'current'],
      ['resolved', 'Vorfall', 'past'],
      ['in_progress', 'Wartung', 'current'],
      ['resolved', 'Wartung', 'past'],
      ['scheduled', 'Wartung', 'upcoming'],
    ])('ordnet %s (%s) der Sektion %s zu', async (status, kind, section) => {
      const make = kind === 'Wartung' ? maint : inc
      const { wrapper } = await mountWith([make(1, status)])
      const where = { current: currentTitles(wrapper), past: pastTitles(wrapper), upcoming: upcomingTitles(wrapper) }
      for (const [name, list] of Object.entries(where)) {
        expect(list).toEqual(name === section ? ['Vorfall 1'] : [])
      }
    })

    // 4a: Die „Alles im grünen Bereich“-Karte entfällt, der Gesamtstatus
    // oben sagt es (Form + Text), und ohne offenen Vorfall gibt es keinen Abschnitt.
    it('zeigt ohne offene Vorfälle keinen Abschnitt „Aktuelle Vorfälle“, der Gesamtstatus sagt „in Ordnung“', async () => {
      const { wrapper } = await mountWith([inc(1, 'resolved'), inc(2, 'resolved')])
      expect(wrapper.find('.all-clear-card').exists()).toBe(false)
      expect(currentSection(wrapper).exists()).toBe(false)
      expect(wrapper.text()).not.toContain('Keine Vorfälle für den aktiven Filter')
      expect(wrapper.text()).toContain('Vergangene Vorfälle')
    })

    it('zeigt den Abschnitt, solange ein Vorfall offen ist', async () => {
      const { wrapper } = await mountWith([inc(1, 'resolved'), inc(2, 'monitoring')])
      expect(wrapper.find('.all-clear-card').exists()).toBe(false)
      expect(currentTitles(wrapper)).toEqual(['Vorfall 2'])
      expect(pastTitles(wrapper)).toEqual(['Vorfall 1'])
    })

    it('blendet die Verlaufssektion ohne behobene Vorfälle aus', async () => {
      const { wrapper } = await mountWith([inc(1, 'investigating')])
      expect(wrapper.find('.past-incidents-list').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('Vergangene Vorfälle')
    })

    it('sortiert den Verlauf nach Ereigniszeit, neueste zuerst', async () => {
      const { wrapper } = await mountWith([
        inc(1, 'resolved', { StartedAt: '2026-07-01T08:00:00Z' }),
        inc(2, 'resolved', { StartedAt: '2026-07-03T08:00:00Z' }),
        maint(3, 'resolved'),
      ])
      expect(pastTitles(wrapper)).toEqual(['Vorfall 2', 'Vorfall 3', 'Vorfall 1'])
    })

    it('wendet die Filter auch auf den Verlauf an und zeigt den Leerzustand nur bei offenen Vorfällen', async () => {
      vi.useFakeTimers()
      const { wrapper } = await mountWith([
        inc(1, 'investigating', { Title: 'Latenz offen' }),
        inc(2, 'resolved', { Title: 'Latenz behoben' }),
        inc(3, 'resolved', { Title: 'Mail behoben' }),
      ])
      await wrapper.find('.search-input').setValue('Mail')
      await vi.advanceTimersByTimeAsync(300)
      expect(pastTitles(wrapper)).toEqual(['Mail behoben'])
      expect(currentTitles(wrapper)).toEqual([])
      expect(wrapper.text()).toContain('Keine Vorfälle für den aktiven Filter')
      expect(wrapper.find('.all-clear-card').exists()).toBe(false)
    })

    it('öffnet den Verlauf-Dialog aus der Verlaufsliste', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce(jsonResponse(dashboardFixture({ incidents: [inc(7, 'resolved')] })))
        .mockResolvedValue(jsonResponse({ updates: [{ id: 1, status: 'resolved', message: 'Erledigt', created_at: '2026-07-01T11:00:00Z' }] }))
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      await wrapper.find('.past-incident .incident-history-btn').trigger('click')
      await flushPromises()
      expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents/7', expect.anything())
      expect(document.body.textContent).toContain('Erledigt')
    })

    it('lässt im Layout liste behobene Vorfälle in der Zeitleiste und zeigt keine eigene Sektion', async () => {
      const { wrapper } = await mountWith([inc(1, 'resolved'), inc(2, 'investigating')], { ID: 1, Name: 'Acme', PageLayout: 'liste' })
      expect(wrapper.find('.past-incidents-list').exists()).toBe(false)
      const timeline = wrapper.find('.pl-timeline').text()
      expect(timeline).toContain('Vorfall 1')
      expect(timeline).toContain('Vorfall 2')
    })
  })

  describe('Ältere Vorfälle nachladen', () => {
    const resolved = (ID, started) => ({
      ID, Title: `Alt ${ID}`, Message: 'm', Status: 'resolved', IsMaintenance: false,
      Tags: [], Components: [{ ID: 10, Name: 'API' }], StartedAt: started, CreatedAt: started,
    })
    const pastTitles = (w) => w.findAll('.past-incident-title').map(e => e.text().trim())
    const moreButton = (w) => w.find('[data-test="load-more-incidents"]')
    const listCalls = (fetchMock) => fetchMock.mock.calls.filter(([url]) => url.includes('/incidents?'))

    // Dashboard liefert 2 behobene und meldet weitere; die Liste liefert die nächsten.
    const fetchWith = ({ more = true, older = [resolved(3, '2026-05-01T10:00:00Z')], listError = false } = {}) => vi.fn(async (url) => {
      if (url.includes('/availability')) return jsonResponse({}, 404)
      if (url.includes('/incidents?')) {
        if (listError) throw new Error('Netzwerk')
        return jsonResponse(older)
      }
      if (url.includes('/translations')) return jsonResponse({}, 404)
      return jsonResponse(dashboardFixture({
        incidents: [resolved(1, '2026-07-01T10:00:00Z'), resolved(2, '2026-06-01T10:00:00Z')],
        incidents_more: more,
      }))
    })

    it('zeigt ohne weitere Vorfälle keinen Knopf', async () => {
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchWith({ more: false }) })
      expect(pastTitles(wrapper)).toEqual(['Alt 1', 'Alt 2'])
      expect(moreButton(wrapper).exists()).toBe(false)
    })

    it('lädt beim Klick die nächste Seite behobener Vorfälle und hängt sie an', async () => {
      const fetchMock = fetchWith()
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(moreButton(wrapper).text()).toContain('Ältere Vorfälle laden')

      await moreButton(wrapper).trigger('click')
      await flushPromises()

      expect(listCalls(fetchMock).map(([url]) => url)).toEqual([
        '/api/pages/1/incidents?is_maintenance=any&state=resolved&limit=50&offset=2',
      ])
      expect(pastTitles(wrapper)).toEqual(['Alt 1', 'Alt 2', 'Alt 3'])
      // weniger als eine volle Seite: nichts mehr zu holen
      expect(moreButton(wrapper).exists()).toBe(false)
    })

    it('behält den Knopf bei voller Seite und zählt den nächsten Offset weiter', async () => {
      const full = Array.from({ length: 50 }, (_, i) => resolved(1000 + i, '2026-04-01T10:00:00Z'))
      const fetchMock = fetchWith({ older: full })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

      await moreButton(wrapper).trigger('click')
      await flushPromises()
      expect(pastTitles(wrapper)).toHaveLength(52)
      expect(moreButton(wrapper).exists()).toBe(true)

      await moreButton(wrapper).trigger('click')
      await flushPromises()
      expect(listCalls(fetchMock).map(([url]) => url).at(-1)).toBe('/api/pages/1/incidents?is_maintenance=any&state=resolved&limit=50&offset=52')
    })

    it('mehrere Nachladeschritte sammeln alle Seiten, keine ersetzt die vorige', async () => {
      const first = Array.from({ length: 50 }, (_, i) => resolved(1000 + i, '2026-04-01T10:00:00Z'))
      const second = [resolved(2000, '2026-03-01T10:00:00Z'), resolved(2001, '2026-03-01T09:00:00Z')]
      let call = 0
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('/availability') || url.includes('/translations')) return jsonResponse({}, 404)
        if (url.includes('/incidents?')) return jsonResponse(call++ === 0 ? first : second)
        return jsonResponse(dashboardFixture({ incidents: [resolved(1, '2026-07-01T10:00:00Z')], incidents_more: true }))
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

      await moreButton(wrapper).trigger('click')
      await flushPromises()
      await moreButton(wrapper).trigger('click')
      await flushPromises()

      const titles = pastTitles(wrapper)
      expect(titles).toHaveLength(1 + 50 + 2)
      expect(titles).toContain('Alt 1000')
      expect(titles).toContain('Alt 2001')
    })

    it('zeigt einen Fehler und behält den Knopf, wenn das Nachladen scheitert', async () => {
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchWith({ listError: true }) })
      await moreButton(wrapper).trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-test="load-more-error"]').text()).toContain('konnten nicht geladen werden')
      expect(moreButton(wrapper).exists()).toBe(true)
      expect(pastTitles(wrapper)).toEqual(['Alt 1', 'Alt 2'])
    })

    it('zählt für den Offset nur behobene Vorfälle, offene und Nachgeladene nicht doppelt', async () => {
      const open = { ...resolved(9, '2026-07-02T10:00:00Z'), Status: 'investigating' }
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('/availability') || url.includes('/translations')) return jsonResponse({}, 404)
        if (url.includes('/incidents?')) return jsonResponse([resolved(2, '2026-06-01T10:00:00Z'), resolved(3, '2026-05-01T10:00:00Z')])
        return jsonResponse(dashboardFixture({
          incidents: [open, resolved(1, '2026-07-01T10:00:00Z'), resolved(2, '2026-06-01T10:00:00Z')],
          incidents_more: true,
        }))
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

      await moreButton(wrapper).trigger('click')
      await flushPromises()

      expect(listCalls(fetchMock).map(([url]) => url)).toEqual([
        '/api/pages/1/incidents?is_maintenance=any&state=resolved&limit=50&offset=2', // zwei behobene, der offene zählt nicht
      ])
      // Alt 2 liegt schon im Dashboard und erscheint nicht ein zweites Mal.
      expect(pastTitles(wrapper).filter(t => t === 'Alt 2')).toHaveLength(1)
      expect(pastTitles(wrapper)).toContain('Alt 3')
    })

    it('sperrt den Knopf während des Ladens und nimmt den Fehler beim nächsten Versuch zurück', async () => {
      let attempt = 0
      let release
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('/availability') || url.includes('/translations')) return jsonResponse({}, 404)
        if (url.includes('/incidents?')) {
          attempt += 1
          if (attempt === 1) throw new Error('Netzwerk')
          await new Promise(resolve => { release = resolve })
          return jsonResponse(Array.from({ length: 50 }, (_, i) => resolved(2000 + i, '2026-05-01T10:00:00Z')))
        }
        return jsonResponse(dashboardFixture({ incidents: [resolved(1, '2026-07-01T10:00:00Z')], incidents_more: true }))
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

      await moreButton(wrapper).trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-test="load-more-error"]').exists()).toBe(true)

      await moreButton(wrapper).trigger('click')
      expect(moreButton(wrapper).attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-test="load-more-error"]').exists()).toBe(false) // beim neuen Versuch weg
      release()
      await flushPromises()
      expect(moreButton(wrapper).attributes('disabled')).toBeUndefined()
    })

    it('die regelmäßige Aktualisierung überschreibt nach dem Nachladen nicht die Angabe "mehr vorhanden"', async () => {
      vi.useFakeTimers()
      let dashboardLoads = 0
      const full = Array.from({ length: 50 }, (_, i) => resolved(1000 + i, '2026-04-01T10:00:00Z'))
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('/availability') || url.includes('/translations')) return jsonResponse({}, 404)
        if (url.includes('/incidents?')) return jsonResponse(full)
        dashboardLoads += 1
        return jsonResponse(dashboardFixture({
          incidents: [resolved(1, '2026-07-01T10:00:00Z')],
          incidents_more: dashboardLoads === 1, // spätere Dashboards melden nichts mehr
        }))
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      await moreButton(wrapper).trigger('click')
      await flushPromises()
      expect(moreButton(wrapper).exists()).toBe(true) // volle Seite: weitere möglich

      await vi.advanceTimersByTimeAsync(60000)
      await flushPromises()
      expect(dashboardLoads).toBeGreaterThan(1)
      expect(moreButton(wrapper).exists()).toBe(true)
    })

    it('behält nachgeladene Vorfälle bei der regelmäßigen Aktualisierung', async () => {
      vi.useFakeTimers()
      const fetchMock = fetchWith()
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      await moreButton(wrapper).trigger('click')
      await flushPromises()
      expect(pastTitles(wrapper)).toHaveLength(3)

      await vi.advanceTimersByTimeAsync(60000)
      await flushPromises()
      expect(dashboardCalls(fetchMock).length).toBeGreaterThan(1)
      expect(pastTitles(wrapper)).toEqual(['Alt 1', 'Alt 2', 'Alt 3'])
    })
  })

  describe('Mehrfachauswahl und Suchsyntax', () => {
    const fixture = () => dashboardFixture({
      components: [
        { ID: 10, Name: 'API', Description: 'Frankfurt Datenbank', Status: 'operational', Tags: [{ Name: 'DE' }], Children: [] },
        { ID: 11, Name: 'Web', Description: 'London Frontend', Status: 'degraded_performance', Tags: [{ Name: 'US' }], Children: [] },
        { ID: 12, Name: 'Edge', Description: 'Paris Datenbank', Status: 'major_outage', Tags: [{ Name: 'FR' }], Children: [] },
        { ID: 13, Name: 'Mail', Description: '', Status: 'operational', Tags: [{ Name: 'US' }], Children: [] },
      ],
    })
    const mountFilter = () => mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fixture())) })
    const names = (w) => w.findAll('.component-tree-node-stub').map(n => n.text())
    const region = (w, name) => w.findAll('.tag-group')[0].findAll('.filter-tag').find(b => b.text() === name)
    const status = (w, key) => w.find(`.filter-tag.status-tag-${key}`)
    const search = async (w, text) => {
      await w.find('.search-input').setValue(text)
      await vi.advanceTimersByTimeAsync(300)
    }

    it('zeigt bei zwei aktiven Regionen Komponenten beider Regionen (ODER)', async () => {
      const { wrapper } = await mountFilter()
      await region(wrapper, 'DE').trigger('click')
      await region(wrapper, 'FR').trigger('click')

      expect(names(wrapper)).toEqual(['API', 'Edge'])
      expect(region(wrapper, 'DE').classes()).toContain('active')
      expect(region(wrapper, 'FR').classes()).toContain('active')
      expect(region(wrapper, 'US').classes()).not.toContain('active')
    })

    it('verknüpft Regionen- und Status-Mehrfachauswahl mit UND zwischen den Gruppen', async () => {
      const { wrapper } = await mountFilter()
      await region(wrapper, 'DE').trigger('click')
      await region(wrapper, 'US').trigger('click')
      await status(wrapper, 'operational').trigger('click')
      expect(names(wrapper)).toEqual(['API', 'Mail'])

      await status(wrapper, 'degraded_performance').trigger('click')
      expect(names(wrapper)).toEqual(['API', 'Web', 'Mail'])
    })

    it('schaltet aktive Chips per Klick ab, der letzte führt zu „Alle“', async () => {
      const { wrapper } = await mountFilter()
      await region(wrapper, 'DE').trigger('click')
      await region(wrapper, 'FR').trigger('click')
      await region(wrapper, 'DE').trigger('click')
      expect(names(wrapper)).toEqual(['Edge'])
      expect(region(wrapper, 'Alle').classes()).not.toContain('active')

      await region(wrapper, 'FR').trigger('click')
      expect(names(wrapper)).toHaveLength(4)
      expect(region(wrapper, 'Alle').classes()).toContain('active')

      await status(wrapper, 'major_outage').trigger('click')
      await status(wrapper, 'major_outage').trigger('click')
      expect(status(wrapper, 'all').classes()).toContain('active')
      expect(names(wrapper)).toHaveLength(4)
    })

    it('leert die Mehrfachauswahl, wenn „Alle“ gewählt wird', async () => {
      const { wrapper } = await mountFilter()
      await region(wrapper, 'DE').trigger('click')
      await region(wrapper, 'FR').trigger('click')
      await status(wrapper, 'operational').trigger('click')
      await status(wrapper, 'all').trigger('click')
      await region(wrapper, 'Alle').trigger('click')

      expect(names(wrapper)).toHaveLength(4)
      expect(region(wrapper, 'DE').classes()).not.toContain('active')
      expect(status(wrapper, 'operational').classes()).not.toContain('active')
    })

    it.each(['Frankfurt AND Datenbank', 'Frankfurt UND Datenbank', 'frankfurt and datenbank', 'Frankfurt und DATENBANK', 'Frankfurt Datenbank'])(
      'sucht „%s“ als UND', async (q) => {
        vi.useFakeTimers()
        const { wrapper } = await mountFilter()
        await search(wrapper, q)
        expect(names(wrapper)).toEqual(['API'])
      })

    it.each(['London OR Paris', 'London ODER Paris', 'london oder paris', 'London or PARIS'])(
      'sucht „%s“ als ODER', async (q) => {
        vi.useFakeTimers()
        const { wrapper } = await mountFilter()
        await search(wrapper, q)
        expect(names(wrapper)).toEqual(['Web', 'Edge'])
      })

    it('wertet UND vor ODER aus', async () => {
      vi.useFakeTimers()
      const { wrapper } = await mountFilter()
      await search(wrapper, 'London OR Paris AND Datenbank')
      expect(names(wrapper)).toEqual(['Web', 'Edge'])
      await search(wrapper, 'London OR Paris AND Frontend')
      expect(names(wrapper)).toEqual(['Web'])
    })

    it('behandelt Phrasen und quotierte Operatoren als normale Terme', async () => {
      vi.useFakeTimers()
      const { wrapper } = await mountFilter()
      await search(wrapper, '"London Frontend"')
      expect(names(wrapper)).toEqual(['Web'])
      await search(wrapper, '"und"')
      expect(names(wrapper)).toEqual([])
    })

    it('wendet die Suchsyntax auch auf Vorfälle an (Titel und Nachricht)', async () => {
      vi.useFakeTimers()
      const { wrapper } = await mountFilter()
      const cards = () => (currentSection(wrapper).exists() ? currentCards(wrapper) : [])
      await search(wrapper, 'down AND antwortet')
      expect(cards()).toHaveLength(1)
      await search(wrapper, 'down AND gibtesnicht')
      expect(cards()).toHaveLength(0)
    })

    it('verträgt lose Operatoren, reine Operatoren und offene Anführungszeichen', async () => {
      vi.useFakeTimers()
      const { wrapper } = await mountFilter()
      await search(wrapper, 'Frankfurt AND')
      expect(names(wrapper)).toEqual(['API'])
      await search(wrapper, 'OR Paris')
      expect(names(wrapper)).toEqual(['Edge'])
      await search(wrapper, 'AND')
      expect(names(wrapper)).toHaveLength(4)
      await search(wrapper, '"London Front')
      expect(names(wrapper)).toEqual(['Web'])
    })

    it('blendet bei aktiver Region Komponenten und Vorfälle ohne Tags aus', async () => {
      const data = fixture()
      data.components.push({ ID: 14, Name: 'Untagged', Status: 'operational', Children: [] })
      data.incidents[0].Tags = undefined
      const { wrapper } = await mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(data)) })
      await region(wrapper, 'DE').trigger('click')

      expect(names(wrapper)).toEqual(['API'])
      expect(currentCards(wrapper)).toHaveLength(0)
    })

    it('zeigt bei aktiven Regionen genau die Vorfälle mit passendem Tag und verknüpft sie per UND mit dem Status', async () => {
      const data = fixture()
      const inc = (ID, Title, Status, ...tags) => ({
        ID, Title, Message: '', Status, IsMaintenance: false, Tags: tags.map(Name => ({ Name })), Components: [], CreatedAt: '2026-07-01T10:00:00Z',
      })
      data.incidents = [
        ...data.incidents.slice(1),
        inc(200, 'Vorfall DE', 'investigating', 'DE', 'XX'),
        inc(201, 'Vorfall FR', 'monitoring', 'FR'),
        inc(202, 'Vorfall US', 'investigating', 'US'),
      ]
      const { wrapper } = await mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(data)) })
      const titles = () => currentCards(wrapper).map(c => c.text())
      const has = (list, title) => list.some(t => t.includes(title))

      await region(wrapper, 'DE').trigger('click')
      await region(wrapper, 'FR').trigger('click')
      expect(titles()).toHaveLength(2)
      expect(has(titles(), 'Vorfall DE')).toBe(true)
      expect(has(titles(), 'Vorfall FR')).toBe(true)
      expect(has(titles(), 'Vorfall US')).toBe(false)

      await status(wrapper, 'operational').trigger('click')
      expect(titles()).toHaveLength(0)
    })

    it('„Filter zurücksetzen“ setzt die Suche sofort zurück, ohne den Debounce abzuwarten', async () => {
      vi.useFakeTimers()
      const { wrapper } = await mountFilter()
      await search(wrapper, 'Datenbank')
      expect(names(wrapper)).toHaveLength(2)
      await wrapper.find('.clear-all-btn').trigger('click')

      expect(names(wrapper)).toHaveLength(4)
      expect(currentCards(wrapper)).toHaveLength(1)
    })

    it('zeigt die Trefferzahl auch bei reiner Mehrfachauswahl', async () => {
      const { wrapper } = await mountFilter()
      expect(wrapper.find('.filter-result-count').exists()).toBe(false)
      await region(wrapper, 'DE').trigger('click')
      await region(wrapper, 'FR').trigger('click')
      expect(wrapper.find('.filter-result-count').text()).toContain('2 Komponente(n)')
    })

    it('„Filter zurücksetzen“ leert Suche, Regionen und Status', async () => {
      vi.useFakeTimers()
      const { wrapper } = await mountFilter()
      await search(wrapper, 'Datenbank')
      await region(wrapper, 'DE').trigger('click')
      await region(wrapper, 'FR').trigger('click')
      await status(wrapper, 'operational').trigger('click')
      await status(wrapper, 'major_outage').trigger('click')
      await wrapper.find('.clear-all-btn').trigger('click')
      await vi.advanceTimersByTimeAsync(300)

      expect(wrapper.find('.search-input').element.value).toBe('')
      expect(names(wrapper)).toHaveLength(4)
      expect(region(wrapper, 'Alle').classes()).toContain('active')
      expect(status(wrapper, 'all').classes()).toContain('active')
      expect(wrapper.find('.filter-result-count').exists()).toBe(false)
    })

    describe('Hilfe-Tooltip', () => {
      it('verknüpft das Suchfeld per aria-describedby mit dem Hilfetext samt Beispielen', async () => {
        const { wrapper } = await mountFilter()
        const id = wrapper.find('.search-input').attributes('aria-describedby')
        const help = wrapper.find(`#${id}`)
        expect(help.exists()).toBe(true)
        expect(help.attributes('role')).toBe('tooltip')
        expect(help.text()).toContain('Frankfurt UND Datenbank')
        expect(help.text()).toContain('London ODER Paris')
      })

      it('öffnet und schließt per Button (Klick/Tap) und mit Escape', async () => {
        const { wrapper } = await mountFilter()
        const btn = wrapper.find('.search-help-btn')
        const help = wrapper.find('.search-help')
        const shown = () => !(help.attributes('style') || '').includes('display: none')
        expect(btn.element.tagName).toBe('BUTTON')
        expect(btn.attributes('aria-label')).toBe('Hilfe zur Suche')
        expect(btn.attributes('aria-expanded')).toBe('false')
        expect(shown()).toBe(false)

        await btn.trigger('click')
        expect(btn.attributes('aria-expanded')).toBe('true')
        expect(shown()).toBe(true)

        await btn.trigger('keydown', { key: 'Escape' })
        expect(btn.attributes('aria-expanded')).toBe('false')
        expect(shown()).toBe(false)

        await btn.trigger('click')
        await wrapper.find('.search-input').trigger('keydown', { key: 'Escape' })
        expect(shown()).toBe(false)

        await btn.trigger('click')
        await btn.trigger('keydown', { key: 'Enter' })
        expect(shown()).toBe(true)
      })

      it('liefert die Hilfetexte auch auf Englisch', async () => {
        i18n.global.locale.value = 'en'
        const { wrapper } = await mountFilter()
        expect(wrapper.find('.search-help-btn').attributes('aria-label')).toBe('Search help')
        expect(wrapper.find('.search-help').text()).toContain('Frankfurt AND database')
      })
    })
  })

  it('öffnet den Incident-Verlauf und zeigt Updates neueste zuerst', async () => {
    const fetchMock = routedFetch({
      detail: {
        updates: [
          { id: 1, status: 'investigating', message: 'Untersuchung läuft', created_at: '2026-07-01T10:00:00Z' },
          { id: 2, status: 'resolved', message: 'Behoben', created_at: '2026-07-01T12:00:00Z' },
        ],
      },
    })
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    // Im Abschnitt „Aktuelle Vorfälle“ liegt der Investigating-Incident.
    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents/100', expect.anything())
    // Das History-Modal wird via <Teleport to="body"> gerendert und liegt
    // damit außerhalb des von wrapper.find() durchsuchten Baums.
    const body = new DOMWrapper(document.body)
    const rows = body.findAll('.tl-row')
    // zusätzlich die Erstmeldung als ältester Eintrag.
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('Behoben')
    expect(rows[1].text()).toContain('Untersuchung')
    expect(rows[2].text()).toContain('Erstmeldung')
  })

  it('zeigt vor dem ersten Update die Erstmeldung als Verlaufseintrag', async () => {
    const fetchMock = routedFetch({ detail: { updates: [] } })
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()
    const body = new DOMWrapper(document.body)
    const rows = body.findAll('.tl-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('API antwortet nicht')
    expect(body.text()).not.toContain('Noch keine Updates')
  })

  it('behält die Erstmeldung als ältesten Verlaufseintrag auch nach Updates', async () => {
    const updates = [
      { id: 1, status: 'identified', message: 'Ursache gefunden', created_at: '2026-07-01T11:00:00Z' },
      { id: 2, status: 'resolved', message: 'Wieder da', created_at: '2026-07-01T12:00:00Z' },
    ]
    const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ detail: { updates } }) })
    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()
    const rows = new DOMWrapper(document.body).findAll('.tl-row')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('Wieder da')
    expect(rows[2].text()).toContain('API antwortet nicht')
    expect(rows[2].text()).toContain('Erstmeldung')
    expect(rows[2].find('.st-badge').exists()).toBe(false)
    expect(rows[2].find('.tl-line').exists()).toBe(false)
    expect(rows[1].find('.tl-line').exists()).toBe(true)
  })

  it('zeigt auf der Karte Erstmeldung und das letzte Update gekennzeichnet', async () => {
    const updates = [
      { id: 1, status: 'identified', message: 'Ursache gefunden', created_at: '2026-07-01T11:00:00Z' },
      { id: 2, status: 'resolved', message: 'Wieder da', created_at: '2026-07-01T12:00:00Z' },
    ]
    const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ detail: { updates } }) })
    const card = currentCards(wrapper)[0]
    expect(card.find('.sp-incident-message').text()).toBe('API antwortet nicht')
    const items = card.findAll('.sp-timeline-item')
    expect(items[0].find('.sp-timeline-latest').text()).toBe('Letztes Update')
    expect(items[0].find('.sp-timeline-message').text()).toBe('Wieder da')
    expect(items[0].find('time').exists()).toBe(true)
    expect(items[1].find('.sp-timeline-latest').exists()).toBe(false)
  })

  it('zeigt ohne Updates und ohne Erstmeldung weiter den Leerzustand', async () => {
    const incidents = [{ ID: 100, Title: 'Leer', Message: '', Status: 'investigating', IsMaintenance: false, Tags: [], Components: [], CreatedAt: '2026-07-01T10:00:00Z' }]
    const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ dashboard: dashboardFixture({ incidents }), detail: { updates: [] } }) })
    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()
    const body = new DOMWrapper(document.body)
    expect(body.findAll('.tl-row')).toHaveLength(0)
    expect(body.text()).toContain('Noch keine Updates')
  })

  describe('Ereigniszeit statt Erstellzeit', () => {
    const base = { Message: 'm', Tags: [], Components: [], CreatedAt: '2026-10-22T01:06:00Z' }
    const fmt = (iso) => new Date(iso).toLocaleString('de-DE')
    const mountWith = (incidents) => mountStatusPage({
      fetchImpl: routedFetch({ dashboard: dashboardFixture({ incidents }) }),
    })

    it('zeigt bei Vorfällen StartedAt statt CreatedAt, ohne StartedAt weiter CreatedAt', async () => {
      i18n.global.locale.value = 'de'
      const { wrapper } = await mountWith([
        { ...base, ID: 1, Title: 'Nachgetragen', Status: 'resolved', StartedAt: '2026-10-02T08:00:00Z' },
        { ...base, ID: 2, Title: 'Neu', Status: 'investigating' },
      ])
      // der behobene Vorfall steht im Verlauf, der offene als Karte.
      const past = wrapper.find('.past-incident')
      expect(past.text()).toContain(fmt('2026-10-02T08:00:00Z'))
      expect(past.text()).not.toContain(fmt('2026-10-22T01:06:00Z'))
      expect(currentCards(wrapper)[0].text()).toContain(fmt('2026-10-22T01:06:00Z'))
    })

    it.each(['resolved', 'in_progress'])('zeigt bei Wartung (%s) Start–Ende statt CreatedAt', async (Status) => {
      i18n.global.locale.value = 'de'
      const { wrapper } = await mountWith([
        { ...base, ID: 3, Title: 'Wartung', Status, IsMaintenance: true, ScheduledStartAt: '2026-10-02T02:00:00Z', ScheduledEndAt: '2026-10-02T04:00:00Z' },
      ])
      const card = Status === 'resolved' ? wrapper.find('.past-incident') : currentCards(wrapper)[0]
      const start = new Date('2026-10-02T02:00:00Z').toLocaleString('de-DE', { dateStyle: 'medium', timeStyle: 'short' })
      expect(card.text()).toContain(start)
      expect(card.text()).not.toContain(fmt('2026-10-22T01:06:00Z'))
    })
  })

  describe('Status-Übersetzung', () => {
    const incident = (Status, extra = {}) => ({
      ID: 200, Title: 'Wartung X', Message: 'm', Status, IsMaintenance: true,
      Tags: [], Components: [], CreatedAt: '2026-07-01T10:00:00Z', ...extra,
    })
    const mountWith = (incidents, updates = []) => mountStatusPage({
      fetchImpl: routedFetch({ dashboard: dashboardFixture({ incidents }), detail: { updates } }),
    })
    const openHistory = async (wrapper) => {
      await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
      await flushPromises()
      return new DOMWrapper(document.body).findAll('.tl-row')
    }

    it('Karte für in_progress zeigt das Wartungs-Badge, resolved steht im Verlauf', async () => {
      const { wrapper } = await mountWith([incident('resolved', { ID: 1 }), incident('in_progress', { ID: 2 })])
      expect(currentCards(wrapper)).toHaveLength(1)
      const badge = currentCards(wrapper)[0].find('.st-badge')
      expect(badge.classes()).toContain('st-badge--maintenance')
      expect(badge.text()).toBe('Wartung läuft')
      expect(wrapper.find('.past-incident').text()).toContain('Wartung X')
    })

    it('activeIncidents enthält nur offene Vorfälle (nicht resolved)', async () => {
      const { wrapper } = await mountWith([
        incident('resolved', { ID: 1 }), incident('investigating', { ID: 3 }),
      ])
      expect(wrapper.vm.activeIncidents.map(i => i.ID)).toEqual([3])
    })

    // 4a: Farbe und Form kommen aus Klassen (Stufe des Status), nicht aus Inline-Styles.
    it.each([
      ['scheduled', 'maintenance'], ['in_progress', 'maintenance'], ['resolved', 'operational'],
      ['investigating', 'major'], ['identified', 'partial'], ['monitoring', 'degraded'],
    ])('stuft %s im Verlauf als %s ein (Punkt und Badge)', async (status, level) => {
      const { wrapper } = await mountWith([incident('investigating')], [
        { id: 1, status, message: 'x', created_at: '2026-07-01T12:00:00Z' },
      ])
      const rows = await openHistory(wrapper)
      expect(rows[0].find('.tl-dot').classes()).toContain(`tl-dot--${level}`)
      expect(rows[0].find('.st-badge').classes()).toContain(`st-badge--${level}`)
      expect(rows[0].find('.tl-dot').attributes('style')).toBeUndefined()
      expect(rows[0].find('svg.status-icon').exists()).toBe(true)
    })

    it.each([
      ['de', 'scheduled', 'Geplant'], ['de', 'in_progress', 'Wartung läuft'],
      ['en', 'scheduled', 'Scheduled'], ['en', 'in_progress', 'Maintenance in progress'],
    ])('übersetzt %s/%s im Verlauf', async (loc, status, label) => {
      i18n.global.locale.value = loc
      const { wrapper } = await mountWith([incident('investigating')], [
        { id: 1, status, message: 'x', created_at: '2026-07-01T12:00:00Z' },
      ])
      const rows = await openHistory(wrapper)
      expect(rows[0].find('.st-badge').text()).toBe(label)
    })

    it('zeigt unbekannte Status weiter als Rohwert (Badge und Verlauf)', async () => {
      const { wrapper } = await mountWith([incident('foo_bar')], [
        { id: 1, status: 'foo_bar', message: 'x', created_at: '2026-07-01T12:00:00Z' },
      ])
      const badge = currentCards(wrapper)[0].find('.sp-incident-head .st-badge')
      expect(badge.text()).toBe('foo_bar')
      // Unbekannte Status bekommen keine Statusfarbe, sondern die neutrale Darstellung.
      expect(badge.classes()).toContain('st-badge--unknown')
      const rows = await openHistory(wrapper)
      expect(rows[0].find('.st-badge').text()).toBe('foo_bar')
      expect(rows[0].find('.tl-dot').classes()).toContain('tl-dot--unknown')
    })

    it('lässt den resolved-Pfad unverändert', async () => {
      const { wrapper } = await mountWith([incident('resolved')])
      const badge = wrapper.find('.past-incident .st-badge')
      expect(badge.text()).toBe('Behoben')
      expect(badge.classes()).toContain('st-badge--operational')
    })
  })

  it('zeigt eine Fehlermeldung im Verlauf-Modal, wenn das Laden fehlschlägt', async () => {
    const fetchMock = routedFetch({ detailError: new Error('Netzwerkfehler') })
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })

    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()

    const body = new DOMWrapper(document.body)
    expect(body.find('.history-body .alert-error').text()).toContain('Verlauf konnte nicht geladen werden')
  })

  it('beschriftet das Verlauf-Modal über seine Überschrift', async () => {
    const { wrapper } = await mountStatusPage()
    const body = new DOMWrapper(document.body)

    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()

    const labelId = body.find('.history-modal').attributes('aria-labelledby')
    expect(labelId).toBeTruthy()
    const title = document.getElementById(labelId)
    expect(title).not.toBeNull()
    expect(title.tagName).toBe('H3')
    expect(title.textContent.trim()).not.toBe('')
  })

  it('schließt das Verlauf-Modal beim Klick auf Schließen', async () => {
    const { wrapper } = await mountStatusPage()
    const body = new DOMWrapper(document.body)

    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()
    expect(body.find('.history-modal').exists()).toBe(true)

    await body.find('.history-close').trigger('click')
    await flushPromises()

    expect(body.find('.history-modal').exists()).toBe(false)
  })

  describe('Abonnement-Zustände', () => {
    const manager = (wrapper) => wrapper.findComponent(SubscriptionManagerStub)

    it('A: ausgeloggt zeigt nur "Jetzt abonnieren"', async () => {
      const { wrapper } = await mountStatusPage()

      expect(wrapper.find('.btn-subscribe-cta--outline').exists()).toBe(false)
      expect(wrapper.find('.subscribe-no-sub').exists()).toBe(false)
      expect(wrapper.find('.btn-subscribe-cta').text()).toContain('Jetzt abonnieren')
    })

    it('eingeloggt, Zustand noch unbekannt: weder Verwalten-Knopf noch Hinweis', async () => {
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper } = await mountStatusPage()

      expect(wrapper.find('.btn-subscribe-cta--outline').exists()).toBe(false)
      expect(wrapper.find('.subscribe-no-sub').exists()).toBe(false)
    })

    it('B: eingeloggt ohne Abonnement zeigt nur "Jetzt abonnieren" und den neutralen Hinweis', async () => {
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper } = await mountStatusPage()

      manager(wrapper).vm.$emit('subscription-state', false)
      await flushPromises()

      expect(wrapper.find('.btn-subscribe-cta--outline').exists()).toBe(false)
      expect(wrapper.find('.subscribe-no-sub').text()).toBe('Du hast noch kein Abonnement für diese Seite.')
      expect(wrapper.find('.subscribe-no-sub').classes()).not.toContain('error')
    })

    // 4a: „Abonnement verwalten“ gibt es nur einmal, im SubscriptionManager.
    // Der Knopf im Abonnieren-Kasten (früher ein zweiter, gleichlautender) entfällt.
    it('C: eingeloggt mit Abonnement zeigt im Kasten nur "Jetzt abonnieren", Verwalten gibt es nur im Manager', async () => {
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper } = await mountStatusPage()

      manager(wrapper).vm.$emit('subscription-state', true)
      await flushPromises()

      expect(wrapper.find('.subscribe-no-sub').exists()).toBe(false)
      expect(wrapper.findAll('.btn-subscribe-cta')).toHaveLength(1)
      expect(wrapper.find('.subscribe-cta').text()).not.toContain('Abonnement verwalten')
      expect(wrapper.findAll('button').filter(b => b.text().includes('Abonnement verwalten'))).toHaveLength(0)
    })

    it('lädt den Abo-Zustand neu, nachdem der Wizard erfolgreich abonniert hat', async () => {
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      const { wrapper } = await mountStatusPage()
      await wrapper.find('.btn-subscribe-cta:not(.btn-subscribe-cta--outline)').trigger('click')

      wrapper.findComponent('.subscribe-wizard-stub').vm.$emit('subscribed')

      expect(refreshMock).toHaveBeenCalledTimes(1)
    })
  })

  it('öffnet den Abo-Editor über das statushafen:open-subscription-manager-Event', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    await mountStatusPage()

    window.dispatchEvent(new CustomEvent('statushafen:open-subscription-manager'))
    await flushPromises()

    expect(openEditAboMock).toHaveBeenCalledTimes(1)
  })

  it('formatiert Zeitstempel im englischen Format, wenn die UI-Sprache Englisch ist (Datum blieb bei Sprachumschaltung deutsch)', async () => {
    i18n.global.locale.value = 'en'
    const { wrapper } = await mountStatusPage()

    // Wartungsfenster-Zeitraum (formatMaintenanceWindow) - Startzeit im
    // englischen Format statt "1. August 2026".
    const maintenanceCard = maintenanceCards(wrapper)[0]
    const expectedStart = new Date('2026-08-01T10:00:00Z').toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' })
    expect(maintenanceCard.text()).toContain(expectedStart)

    // Incident-Zeitstempel (inline toLocaleString ohne Optionen).
    const incidentCard = currentCards(wrapper)[0]
    const expectedIncidentTime = new Date('2026-07-01T10:00:00Z').toLocaleString('en-US')
    expect(incidentCard.text()).toContain(expectedIncidentTime)
  })

  it('pollt die Dashboard-Daten alle 60 Sekunden neu', async () => {
    vi.useFakeTimers()
    const fetchMock = routedFetch()
    await mountStatusPage({ fetchImpl: fetchMock })
    expect(dashboardCalls(fetchMock)).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(60000)

    expect(dashboardCalls(fetchMock)).toHaveLength(2)
  })

  it('räumt Polling-Intervall und Event-Listener beim Unmount auf', async () => {
    vi.useFakeTimers()
    const fetchMock = routedFetch()
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
    const before = fetchMock.mock.calls.length

    wrapper.unmount()
    activeWrapper = null
    await vi.advanceTimersByTimeAsync(60000)

    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('hat keine axe-Funde mit Suche, Vorfällen und Wartungen', async () => {
    const { wrapper } = await mountStatusPage()
    await wrapper.find('.search-input').setValue('API')
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde im Listen-Layout', async () => {
    const fixture = dashboardFixture()
    fixture.page.PageLayout = 'liste'
    const { wrapper } = await mountStatusPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fixture)) })
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde im Verlauf-Modal', async () => {
    const fetchMock = routedFetch({ detail: { updates: [{ id: 1, status: 'investigating', message: 'Untersuchung läuft', created_at: '2026-07-01T10:00:00Z' }] } })
    const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
    await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
    await flushPromises()
    const modal = document.body.querySelector('.history-modal')
    expect(modal).not.toBeNull()
    expect(await axe(modal)).toHaveNoViolations()
  })
  // ===== Redesign 4a: Gesamtstatus oben, Vorfall mit Zeitleiste, 90-Tage-Balken =====
  describe('Redesign 4a: Aufbau der Seite', () => {
    const comps = (...statuses) => statuses.map((Status, i) => ({ ID: 20 + i, Name: `K${i}`, Status, Tags: [], Children: [] }))
    const dash = (components, incidents = []) => dashboardFixture({ components, incidents })
    const mountDash = (d, extra = {}) => mountStatusPage({ fetchImpl: routedFetch({ dashboard: d, ...extra }) })
    const days = (n = 90, over = {}) => Array.from({ length: n }, (_, i) => ({
      date: `2026-07-${String((i % 28) + 1).padStart(2, '0')}`, status: 'operational', availability: 100, downtime_seconds: 0, ...over,
    }))

    it.each([
      [['operational', 'operational'], [], 'Alle Systeme laufen normal', 'operational', '2 von 2 Komponenten laufen normal.'],
      [['operational', 'degraded_performance'], [], 'Eingeschränkte Leistung', 'degraded', '1 von 2 Komponenten laufen normal.'],
      [['partial_outage'], [], 'Teilweise Störung', 'partial', '0 von 1 Komponenten laufen normal.'],
      [['operational', 'major_outage'], [], 'Größere Störung', 'major', '1 von 2 Komponenten laufen normal.'],
      [['operational'], [{ ID: 1, Title: 'W', Message: 'm', Status: 'in_progress', IsMaintenance: true, Components: [], CreatedAt: '2026-07-01T10:00:00Z' }],
        'Wartungsarbeiten laufen', 'maintenance', '1 von 1 Komponenten laufen normal.'],
    ])('zeigt oben den Gesamtstatus %j als Titel, Symbol und Zusammenfassung', async (statuses, incidents, title, level, summary) => {
      const { wrapper } = await mountDash(dash(comps(...statuses), incidents))
      const hero = wrapper.find('.sp-hero')
      expect(hero.classes()).toContain(`sp-hero--${level}`)
      expect(hero.attributes('role')).toBe('status')
      expect(hero.find('h1').text()).toBe(title)
      expect(hero.find('svg.status-icon').exists()).toBe(true)
      expect(hero.find('.sp-hero-sub').text()).toContain(summary)
      expect(hero.find('.sp-hero-sub').text()).toContain('Zuletzt aktualisiert:')
      expect(wrapper.findAll('h1')).toHaveLength(1)
    })

    it('steht der Reihenfolge nach vor der Filterleiste, der Filter vor den Vorfällen und diese vor den Komponenten', async () => {
      const { wrapper } = await mountStatusPage()
      const html = wrapper.html()
      const at = (needle) => html.indexOf(needle)
      expect(at('class="sp-brand"')).toBeGreaterThan(-1)
      expect(at('sp-hero')).toBeLessThan(at('filter-bar'))
      expect(at('filter-bar')).toBeLessThan(at('sp-current-heading'))
      expect(at('sp-current-heading')).toBeLessThan(at('sp-components-heading'))
      expect(at('sp-brand')).toBeLessThan(at('sp-hero'))
    })

    it('zeigt Kundenlogo, sonst den Anfangsbuchstaben, dazu „Systemstatus“', async () => {
      const withLogo = dashboardFixture({ page: { ID: 1, Name: 'Acme', LogoURL: '/x.png', HeaderStyle: 'classic' } })
      const { wrapper } = await mountDash(withLogo)
      expect(wrapper.find('.sp-brand img').attributes('src')).toBe('/x.png')
      expect(wrapper.find('.sp-brand-mark').exists()).toBe(false)
      expect(wrapper.find('.sp-brand-sub').text()).toBe('Systemstatus')
      wrapper.unmount()
      activeWrapper = null
      const { wrapper: w2 } = await mountStatusPage()
      expect(w2.find('.sp-brand-mark').text()).toBe('A')
      expect(w2.find('.sp-brand img').exists()).toBe(false)
    })

    it('zeigt die Beschreibung der Seite im Hero', async () => {
      const d = dashboardFixture({ page: { ID: 1, Name: 'Acme', Description: 'Unser Status', HeaderStyle: 'classic' } })
      const { wrapper } = await mountDash(d)
      expect(wrapper.find('.sp-hero .status-description').text()).toBe('Unser Status')
    })

    it('zeigt ohne Komponenten keine Zusammenfassung', async () => {
      const { wrapper } = await mountDash(dash([]))
      expect(wrapper.find('.sp-hero-sub').exists()).toBe(false)
    })

    it('hat mit Ausnahme der Kundenmarke keinen Inline-Style (nur --primary & Co. auf .content-area)', async () => {
      const d = dashboardFixture({
        page: { ID: 1, Name: 'Acme', PrimaryColor: '#e11d48', HeaderStyle: 'classic' },
        notices: [{ ID: 5, Title: 'H', Message: 'x', Level: 'critical', Collapsed: false, CreatedAt: '2026-07-01T10:00:00Z' }],
      })
      const { wrapper } = await mountDash(d)
      // v-show (Suchhilfe) schreibt nur display: none, das ist kein gestalteter Inline-Style
      const styled = [...wrapper.element.querySelectorAll('[style]')].filter(e => e.getAttribute('style').trim() !== 'display: none;')
      expect(styled).toEqual([wrapper.find('.content-area').element])
      expect(wrapper.find('.content-area').attributes('style')).toContain('--primary:')
    })

    it('ersetzt die alten badge-Klassen und Statuspunkte durch StatusBadge/StatusIcon', async () => {
      const incidents = [
        { ID: 1, Title: 'Aktiv', Message: 'm', Status: 'investigating', IsMaintenance: false, Components: [], CreatedAt: '2026-07-01T10:00:00Z' },
        { ID: 2, Title: 'Alt', Message: 'm', Status: 'resolved', IsMaintenance: false, Components: [], CreatedAt: '2026-07-01T10:00:00Z' },
        { ID: 3, Title: 'Geplant', Message: 'm', Status: 'scheduled', IsMaintenance: true, ScheduledStartAt: '2026-08-01T10:00:00Z', Components: [], CreatedAt: '2026-07-01T10:00:00Z' },
      ]
      const { wrapper } = await mountDash(dash(comps('operational'), incidents))
      expect(wrapper.find('.badge').exists()).toBe(false)
      expect(wrapper.find('.status-pulse').exists()).toBe(false)
      expect(wrapper.findAll('.st-badge').length).toBeGreaterThanOrEqual(3)
      // Statusfilter: Form (Symbol) statt Farbpunkt
      expect(wrapper.find('.filter-tag.status-tag-major_outage svg.status-icon').exists()).toBe(true)
      // Geplante Wartung: Badge „Geplant“ mit Wartungsform
      const planned = maintenanceCards(wrapper)[0].find('.st-badge')
      expect(planned.text()).toBe('Geplant')
      expect(planned.classes()).toContain('st-badge--maintenance')
    })

    it('ersetzt in der Filterleiste „Alle“ ohne Symbol, den Rest mit Symbol', async () => {
      const { wrapper } = await mountStatusPage()
      const tags = wrapper.findAll('.tag-group')[1].findAll('.filter-tag')
      expect(tags[0].find('svg').exists()).toBe(false)
      expect(tags.slice(1).every(t => t.find('svg.status-icon').exists())).toBe(true)
    })

    it('zeigt ohne Komponenten im Baum den Leerzustand', async () => {
      const { wrapper } = await mountDash(dash([]))
      expect(section(wrapper, 'sp-components-heading').find('.empty-state').text()).toContain('Keine Komponenten gefunden.')
    })
  })

  describe('Redesign 4a: aktueller Vorfall mit Zeitleiste', () => {
    const updates = [1, 2, 3, 4, 5].map(n => ({ id: n, status: n === 5 ? 'resolved' : 'identified', message: `Update ${n}`, created_at: `2026-07-01T1${n}:00:00Z` }))

    it('lädt die Updates des aktuellen Vorfalls und zeigt die drei neuesten zuerst', async () => {
      const fetchMock = routedFetch({ detail: { updates } })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents/100', expect.anything())
      const items = currentCards(wrapper)[0].findAll('.sp-timeline-item')
      expect(items.map(i => i.find('.sp-timeline-message').text())).toEqual(['Update 5', 'Update 4', 'Update 3'])
      expect(items[0].find('time').attributes('datetime')).toBe('2026-07-01T15:00:00Z')
      expect(items[0].find('.st-badge--plain').text()).toBe('Behoben')
      expect(currentCards(wrapper)[0].find('ol').attributes('aria-label')).toBe('Updates zu API down')
    })

    it('lädt je Vorfall nur einmal neu, solange Status und Stand gleich bleiben (Polling)', async () => {
      vi.useFakeTimers()
      const fetchMock = routedFetch({ detail: { updates } })
      await mountStatusPage({ fetchImpl: fetchMock })
      const detailCalls = () => fetchMock.mock.calls.filter(([u]) => u.includes('/incidents/') && !u.includes('/update-translations')).length
      expect(detailCalls()).toBe(1)
      await vi.advanceTimersByTimeAsync(60000)
      expect(detailCalls()).toBe(1)
    })

    it('lädt neu, wenn sich der Vorfall ändert (UpdatedAt)', async () => {
      vi.useFakeTimers()
      let stand = '2026-07-01T10:00:00Z'
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('/availability')) return jsonResponse({}, 404)
        if (url.includes('/incidents/')) return jsonResponse({ updates })
        const d = dashboardFixture()
        d.incidents[0].UpdatedAt = stand
        return jsonResponse(d)
      })
      await mountStatusPage({ fetchImpl: fetchMock })
      stand = '2026-07-01T11:00:00Z'
      await vi.advanceTimersByTimeAsync(60000)
      expect(fetchMock.mock.calls.filter(([u]) => u.includes('/incidents/') && !u.includes('/update-translations'))).toHaveLength(2)
    })

    it('begrenzt die Zeitleiste auf drei Vorfälle', async () => {
      const incidents = [1, 2, 3, 4].map(n => ({ ID: 300 + n, Title: `V${n}`, Message: 'm', Status: 'investigating', IsMaintenance: false, Components: [], CreatedAt: '2026-07-01T10:00:00Z' }))
      const fetchMock = routedFetch({ dashboard: dashboardFixture({ incidents }), detail: { updates } })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(fetchMock.mock.calls.filter(([u]) => u.includes('/incidents/') && !u.includes('/update-translations'))).toHaveLength(3)
      expect(currentCards(wrapper)).toHaveLength(4)
      expect(currentCards(wrapper)[3].find('ol').exists()).toBe(false)
    })

    it('zeigt bei Ladefehler einen Hinweis statt der Zeitleiste', async () => {
      const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ detailError: new Error('x') }) })
      expect(currentCards(wrapper)[0].text()).toContain('Die Updates konnten nicht geladen werden.')
      expect(currentCards(wrapper)[0].find('ol').exists()).toBe(false)
    })

    it('zeigt „Lädt“, solange die Updates unterwegs sind, und danach nichts bei leerer Liste', async () => {
      let release
      const gate = new Promise(r => { release = r })
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('/availability')) return jsonResponse({}, 404)
        if (url.includes('/incidents/')) { await gate; return jsonResponse({ updates: [] }) }
        return jsonResponse(dashboardFixture())
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(currentCards(wrapper)[0].text()).toContain('Lädt')
      release()
      await flushPromises()
      expect(currentCards(wrapper)[0].text()).not.toContain('Lädt')
      expect(currentCards(wrapper)[0].find('ol').exists()).toBe(false)
    })

    it('hat keine axe-Funde mit Hero, Vorfall und Zeitleiste', async () => {
      const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ detail: { updates } }) })
      expect(await axe(wrapper)).toHaveNoViolations()
    })
  })

  describe('Redesign 4a: 90-Tage-Verfügbarkeit', () => {
    const days = (over = {}) => Array.from({ length: 90 }, (_, i) => ({
      date: `2026-07-${String((i % 28) + 1).padStart(2, '0')}`, status: 'operational', availability: 100, downtime_seconds: 0, ...over,
    }))

    it('ruft den Endpunkt ohne include auf und zeigt Balken und Prozent für die Gesamtseite', async () => {
      const fetchMock = routedFetch({ availability: { uptime_percent: 99.84, days: days() } })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(fetchMock).toHaveBeenCalledWith('/api/public/pages/acme/availability', expect.anything())
      const sec = section(wrapper, 'sp-availability-heading')
      expect(sec.find('h2').text()).toBe('Verfügbarkeit der letzten 90 Tage')
      expect(sec.findAll('.avail__day')).toHaveLength(90)
      expect(sec.find('.avail__percent').text()).toMatch(/^99,84\s%\sverfügbar$/)
    })

    it('zeigt no_data neutral und bei uptime_percent null keine Prozentzahl', async () => {
      const { wrapper } = await mountStatusPage({
        fetchImpl: routedFetch({ availability: { uptime_percent: null, days: days({ status: 'no_data', availability: null }) } }),
      })
      const sec = section(wrapper, 'sp-availability-heading')
      expect(sec.findAll('.avail__day--none')).toHaveLength(90)
      expect(sec.find('.avail__percent').exists()).toBe(false)
      expect(sec.text()).not.toContain('%')
    })

    it('behandelt ein fehlendes uptime_percent wie null', async () => {
      const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ availability: { days: days() } }) })
      expect(section(wrapper, 'sp-availability-heading').find('.avail__percent').exists()).toBe(false)
    })

    it('reicht den Subscriber-Token an den Endpunkt weiter', async () => {
      const fetchMock = routedFetch({ availability: { uptime_percent: 100, days: days() } })
      await mountStatusPage({ routePath: '/status/acme?token=tok123', fetchImpl: fetchMock })
      expect(fetchMock).toHaveBeenCalledWith('/api/public/pages/acme/availability?token=tok123', expect.anything())
    })

    it('hängt im Listen-Layout include=components an, mit Token zusätzlich', async () => {
      const d = dashboardFixture()
      d.page.PageLayout = 'liste'
      const fetchMock = routedFetch({ dashboard: d })
      await mountStatusPage({ routePath: '/status/acme?token=t%201', fetchImpl: fetchMock })
      expect(fetchMock).toHaveBeenCalledWith('/api/public/pages/acme/availability?token=t%201&include=components', expect.anything())
    })

    it('lässt den Abschnitt bei Fehler oder leerer Antwort weg, die Seite bleibt vollständig', async () => {
      const failing = vi.fn(async (url) => {
        if (url.includes('/availability')) throw new Error('down')
        if (url.includes('/incidents/')) return jsonResponse({ updates: [] })
        return jsonResponse(dashboardFixture())
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: failing })
      expect(section(wrapper, 'sp-availability-heading').exists()).toBe(false)
      expect(wrapper.find('.sp-hero').exists()).toBe(true)
      wrapper.unmount()
      activeWrapper = null
      const { wrapper: leer } = await mountStatusPage({ fetchImpl: routedFetch({ availability: { days: [] } }) })
      expect(section(leer, 'sp-availability-heading').exists()).toBe(false)
    })

    it('lädt die Verfügbarkeit nicht für die private Fehlerseite', async () => {
      const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}, 403))
      await mountStatusPage({ fetchImpl: fetchMock })
      expect(fetchMock.mock.calls.filter(([u]) => u.includes('/availability'))).toHaveLength(0)
    })

    it('hat keine axe-Funde mit Balken', async () => {
      const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ availability: { uptime_percent: 99, days: days({ status: 'degraded_performance', availability: 98 }) } }) })
      expect(await axe(wrapper)).toHaveNoViolations()
    })
  })

  // Kundenmarke (Plan „Risiken pro Paket“, 4a): eine kräftige Markenfarbe bleibt
  // über --primary wirksam und wird nur so weit angepasst, dass weiße Schrift 4,5:1 hält.
  describe('Redesign 4a: Kundenfarbe #e11d48', () => {
    it('setzt --primary aus der Kundenfarbe und stellt die Statusflächen nicht um', async () => {
      const d = dashboardFixture({ page: { ID: 1, Name: 'Acme', PrimaryColor: '#e11d48', HeaderStyle: 'classic' } })
      const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ dashboard: d }) })
      const style = wrapper.find('.content-area').attributes('style')
      expect(style).toContain('--primary: #e11d48')
      expect(style).not.toContain('--ok')
      expect(style).not.toContain('--major')
      expect(wrapper.find('.sp-brand-mark').exists()).toBe(true)
    })
  })
  describe('Redesign 4a: Rest der geänderten Zweige', () => {
    it('leert die Suche über das Kreuz im Suchfeld', async () => {
      const { wrapper } = await mountStatusPage()
      expect(wrapper.find('.search-clear').exists()).toBe(false)
      await wrapper.find('.search-input').setValue('API')
      await wrapper.find('.search-clear').trigger('click')
      expect(wrapper.find('.search-input').element.value).toBe('')
      expect(wrapper.find('.search-clear').exists()).toBe(false)
    })

    it('klappt eingeklappte Ankündigungen hinter einem gemeinsamen Knopf auf und zu', async () => {
      const notices = [
        { ID: 1, Title: 'Offen', Message: 'a', Level: 'warning', Collapsed: false, CreatedAt: '2026-07-01T10:00:00Z' },
        { ID: 2, Title: 'Versteckt', Message: 'b', Level: 'critical', Collapsed: true, CreatedAt: '2026-07-01T10:00:00Z' },
      ]
      const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ dashboard: dashboardFixture({ notices }) }) })
      const sec = section(wrapper, 'sp-notices-heading')
      expect(sec.text()).not.toContain('Versteckt')
      const toggle = sec.find('.notice-collapsed-toggle')
      expect(toggle.attributes('aria-expanded')).toBe('false')
      expect(toggle.text()).toContain('1 weitere Ankündigung(en) anzeigen')
      await toggle.trigger('click')
      expect(sec.text()).toContain('Versteckt')
      expect(sec.find('.notice-critical').exists()).toBe(true)
      expect(toggle.attributes('aria-expanded')).toBe('true')
      expect(toggle.text()).toContain('Weitere Ankündigungen ausblenden')
    })

    it('öffnet den Wizard, wenn der Abo-Manager „neu abonnieren“ meldet', async () => {
      const { wrapper } = await mountStatusPage()
      wrapper.findComponent(SubscriptionManagerStub).vm.$emit('open-wizard')
      await flushPromises()
      expect(wrapper.find('.subscribe-wizard-stub').exists()).toBe(true)
    })

    // toter Code aus der Zeit vor dem Abo-Wizard ist entfernt.
    it('hat keinen toten Abo-Code (publicSubscribe, successMessage) und keine zugehörigen Texte mehr', async () => {
      const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch() })
      expect(wrapper.vm.publicSubscribe).toBeUndefined()
      expect(wrapper.vm.successMessage).toBeUndefined()
      expect(wrapper.find('.alert-success').exists()).toBe(false)
      for (const loc of ['de', 'en']) {
        expect(Object.keys(i18n.global.getLocaleMessage(loc).statusPage).filter(k => k.startsWith('publicSubscribe'))).toEqual([])
      }
    })
  })

  // externe Quellen auf der Statusseite.
  describe('externe Quellen', () => {
    const extComp = (over = {}) => ({
      ID: 20, Name: 'Compute', Status: 'operational', effective_status: 'major_outage', Tags: [], Children: [],
      external_sources: [{ external_component_id: 1, source_id: 2, source_name: 'Example Cloud', name: 'VM', status: 'major_outage', url: 'https://status.example.com', vanished: false, is_group: false, source_outage: false }],
      ...over,
    })
    const extIncident = (over = {}) => ({
      id: 7, source_id: 2, source_name: 'Example Cloud', source_url: 'https://status.example.com', kind: 'incident', external_id: 'x',
      title: 'Region B API errors', status: 'investigating', impact: 'major', closed: false, url: 'https://status.example.com/incidents/x',
      started_at: '2026-10-29T10:00:00Z', updated_at: '2026-10-29T10:30:00Z', affected_components: [{ id: 20, name: 'Compute' }],
      external_components: [], updates: [{ id: 'u1', status: 'investigating', body: 'Wir untersuchen.', display_at: '2026-10-29T10:00:00Z' }], ...over,
    })
    const mountExt = (components, external_incidents) =>
      mountStatusPage({ fetchImpl: routedFetch({ dashboard: dashboardFixture({ components, incidents: [], external_incidents }) }) })

    it('zeigt den effektiven Status im Gesamtstatus statt des eigenen', async () => {
      const { wrapper } = await mountExt([extComp()], [])
      expect(wrapper.find('.sp-hero-title').text()).toBe('Größere Störung')
      expect(wrapper.find('.sp-hero-sub').text()).toContain('0 von 1 Komponenten laufen normal.')
    })

    it('zeigt eine externe Wartung als Wartungsarbeiten (Rohwert under_maintenance)', async () => {
      const { wrapper } = await mountExt([extComp({ effective_status: 'under_maintenance' })], [])
      expect(wrapper.find('.sp-hero-title').text()).toBe('Wartungsarbeiten laufen')
    })

    it('ohne effektiven Status (altes Backend) bleibt der eigene Status', async () => {
      const { wrapper } = await mountExt([extComp({ effective_status: undefined })], [])
      expect(wrapper.find('.sp-hero-title').text()).toBe('Alle Systeme laufen normal')
    })

    it('zeigt den Abschnitt „Meldungen externer Quellen“ nur mit Einträgen', async () => {
      const { wrapper } = await mountExt([extComp()], [extIncident()])
      const sec = section(wrapper, 'ei-heading')
      expect(sec.exists()).toBe(true)
      expect(sec.text()).toContain('Region B API errors')
      expect(sec.text()).toContain('Quelle: Example Cloud')
      expect(sec.find('a').attributes('rel')).toBe('noopener noreferrer nofollow')
      expect(await axe(wrapper)).toHaveNoViolations()
      wrapper.unmount()
      const { wrapper: leer } = await mountExt([extComp()], [])
      expect(section(leer, 'ei-heading').exists()).toBe(false)
    })

    it('rendert Texte externer Meldungen nie als HTML', async () => {
      const evil = '<script>window.__x = 1</script><img src=x onerror="window.__x = 2">'
      const { wrapper } = await mountExt([extComp()], [extIncident({
        title: evil, updates: [{ id: 'u', status: 'investigating', body: evil, display_at: '2026-10-29T10:00:00Z' }],
      })])
      const sec = section(wrapper, 'ei-heading')
      expect(sec.findAll('img, script, [onerror]')).toHaveLength(0)
      expect(sec.text()).toContain(evil)
    })

    // Suche und Filter erfassen auch externe Meldungen.
    describe('Suche, Filter, Verlauf und Zeitleiste', () => {
      const comps = () => [
        { ID: 20, Name: 'Compute', Status: 'major_outage', Tags: [{ Name: 'DE' }], Children: [] },
        { ID: 21, Name: 'Storage', Status: 'operational', Tags: [{ Name: 'US' }], Children: [] },
      ]
      const two = () => [
        extIncident(),
        extIncident({ id: 8, title: 'Backup langsam', affected_components: [{ id: 21, name: 'Storage' }] }),
      ]
      const titles = (w) => w.findAll('.ei__title').map(e => e.text())
      const search = async (w, text) => {
        await w.find('.search-input').setValue(text)
        await vi.advanceTimersByTimeAsync(300)
      }
      beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }))

      it('filtert per Suche nach Titel und betroffener Komponente', async () => {
        const { wrapper } = await mountExt(comps(), two())
        expect(titles(wrapper)).toEqual(['Region B API errors', 'Backup langsam'])
        await search(wrapper, 'backup')
        expect(titles(wrapper)).toEqual(['Backup langsam'])
        await search(wrapper, 'compute')
        expect(titles(wrapper)).toEqual(['Region B API errors'])
        await search(wrapper, 'gibtesnicht')
        expect(section(wrapper, 'ei-heading').exists()).toBe(false)
      })

      it('folgt dem Länderfilter über die Tags der betroffenen Komponenten', async () => {
        const { wrapper } = await mountExt(comps(), two())
        await wrapper.findAll('.tag-group')[0].findAll('.filter-tag').find(b => b.text() === 'US').trigger('click')
        expect(titles(wrapper)).toEqual(['Backup langsam'])
      })

      it('folgt dem Status-Chip über den Status der betroffenen Komponenten', async () => {
        const { wrapper } = await mountExt(comps(), two())
        await wrapper.find('.filter-tag.status-tag-major_outage').trigger('click')
        expect(titles(wrapper)).toEqual(['Region B API errors'])
      })

      it('zählt externe Meldungen im Ergebniszähler mit', async () => {
        const { wrapper } = await mountExt(comps(), two())
        await wrapper.find('.filter-tag.status-tag-major_outage').trigger('click')
        expect(wrapper.find('.filter-result-count').text()).toContain('1 Vorfall/Vorfälle')
      })

      it('öffnet den Verlauf-Dialog mit den Updates als Text (nie HTML)', async () => {
        const evil = '<img src=x onerror="window.__x = 1">'
        const { wrapper } = await mountExt(comps(), [extIncident({
          updates: [
            { id: 'u1', status: 'investigating', body: 'Wir untersuchen.', display_at: '2026-10-29T10:00:00Z' },
            { id: 'u2', status: 'identified', body: evil, display_at: '2026-10-29T11:00:00Z' },
          ],
        })])
        await wrapper.find('.ei__history').trigger('click')
        const modal = document.body.querySelector('.history-modal')
        expect(modal).not.toBeNull()
        expect(modal.querySelector('#history-title').textContent).toBe('Region B API errors')
        expect([...modal.querySelectorAll('.tl-msg')].map(e => e.textContent)).toEqual([evil, 'Wir untersuchen.'])
        expect(modal.querySelectorAll('img, [onerror]')).toHaveLength(0)
        expect(modal.textContent).not.toContain('Erstmeldung')
        expect(await axe(modal)).toHaveNoViolations()
      })

      it('verträgt fehlende Angaben (Tags, affected_components, Updates, Zeiten) bei Filter, Zeitleiste und Verlauf', async () => {
        const cs = [{ ID: 20, Name: 'Compute', Status: 'operational', Children: [{ ID: 22, Name: 'Sub', Status: 'operational', Tags: [{ Name: 'DE' }] }] }]
        const items = [
          extIncident({ id: 1, title: 'Ohne alles', affected_components: undefined, updates: undefined, started_at: null, updated_at: '2026-10-29T09:00:00Z' }),
          extIncident({ id: 2, title: 'Unbekannte Komponente', affected_components: [{ id: 999, name: 'X' }], updates: [] }),
          extIncident({ id: 3, title: 'Wartung', kind: 'maintenance', scheduled_for: '2026-11-01T10:00:00Z', affected_components: [{ id: 22, name: 'Sub' }] }),
        ]
        const fx = dashboardFixture({ components: cs, incidents: [], external_incidents: items })
        fx.page = { ...fx.page, PageLayout: 'liste' }
        const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ dashboard: fx }) })
        expect(wrapper.findAll('.pl-incident')).toHaveLength(3)
        await wrapper.findAll('.pl-link')[2].trigger('click')
        expect(document.body.querySelector('#history-title').textContent).toBe('Ohne alles')
        expect(document.body.querySelector('.history-modal').textContent).toContain('Noch keine Updates.')
        await wrapper.findAll('.pl-link')[0].trigger('click')
        expect(document.body.querySelector('#history-title').textContent).toBe('Wartung')
        await wrapper.findAll('.tag-group')[0].findAll('.filter-tag').find(b => b.text() === 'DE').trigger('click')
        expect(wrapper.findAll('.pl-incident').map(i => i.find('.pl-incident-title').text())).toEqual(['Wartung'])
        await wrapper.find('.filter-tag.status-tag-operational').trigger('click')
        expect(wrapper.findAll('.pl-incident')).toHaveLength(1)
        await wrapper.find('.filter-tag.status-tag-major_outage').trigger('click')
        expect(wrapper.findAll('.pl-incident')).toHaveLength(1)
      })

      it('zeigt im Layout liste externe Meldungen in der Zeitleiste statt im eigenen Abschnitt', async () => {
        const fx = dashboardFixture({ components: comps(), incidents: [], external_incidents: two() })
        fx.page = { ...fx.page, PageLayout: 'liste' }
        const { wrapper } = await mountStatusPage({ fetchImpl: routedFetch({ dashboard: fx }) })
        expect(section(wrapper, 'ei-heading').exists()).toBe(false)
        const items = wrapper.findAll('.pl-incident')
        expect(items.map(i => i.find('.pl-incident-title').text())).toEqual(['Region B API errors', 'Backup langsam'])
        expect(items[0].find('.pl-ext-chip').text()).toContain('Quelle: Example Cloud')
        expect(items[0].find('.pl-incident-msg').text()).toBe('Wir untersuchen.')
        await search(wrapper, 'backup')
        expect(wrapper.findAll('.pl-incident')).toHaveLength(1)
        await wrapper.find('.pl-link').trigger('click')
        expect(document.body.querySelector('#history-title').textContent).toBe('Backup langsam')
      })
    })
  })

  // Übersetzungen auf der öffentlichen Statusseite.
  describe('Übersetzungen', () => {
    const trFixture = (overrides = {}) => ({
      language: 'en',
      default_language: 'de',
      components: { 10: { title: 'API (EN)', text: 'Interface' } },
      incidents: { 100: { title: 'API is down', text: 'API does not respond' } },
      notices: { 5: { title: 'Notice EN', text: 'Notice text EN' } },
      ...overrides,
    })
    const fixtureWithNotice = () => {
      const fx = dashboardFixture({
        notices: [{ ID: 5, Title: 'Hinweis', Message: 'Hinweistext', Level: 'info', Collapsed: false }],
      })
      fx.incidents[0].Components = [{ ID: 10, Name: 'API' }]
      return fx
    }
    // Dashboard, Übersetzungen und restliche Aufrufe; translations: Funktion oder Antwort.
    const withTranslations = ({ dashboard = fixtureWithNotice(), translations = trFixture(), detail = { updates: [] } } = {}) => vi.fn(async (url) => {
      if (url.includes('/translations')) {
        const res = typeof translations === 'function' ? translations(url) : translations
        return res instanceof Error ? Promise.reject(res) : jsonResponse(res)
      }
      if (url.includes('/availability')) return jsonResponse({}, 404)
      if (url.includes('/incidents/')) return jsonResponse(detail)
      return jsonResponse(dashboard)
    })
    const translationCalls = (fetchMock) => fetchMock.mock.calls.map(([url]) => url).filter(url => url.includes('/translations'))
    const enMount = (opts) => {
      i18n.global.locale.value = 'en'
      return mountStatusPage({ fetchImpl: withTranslations(opts) })
    }

    it('ruft die gebündelten Übersetzungen mit der Sprache des Besuchers und dem Token ab', async () => {
      i18n.global.locale.value = 'en'
      const fetchMock = withTranslations()
      await mountStatusPage({ routePath: '/status/acme?token=tok1', fetchImpl: fetchMock })
      expect(translationCalls(fetchMock)).toEqual(['/api/public/pages/acme/translations?lang=en&token=tok1'])
    })

    it('zeigt Komponenten, Vorfälle, betroffene Komponenten und Ankündigungen übersetzt', async () => {
      const { wrapper } = await enMount()
      const text = wrapper.text()
      expect(wrapper.findAll('.component-tree-node-stub').map(n => n.text())).toContain('API (EN)')
      expect(text).toContain('API is down')
      expect(text).toContain('API does not respond')
      expect(text).toContain('Notice EN')
      expect(text).toContain('Notice text EN')
      expect(text).not.toContain('API antwortet nicht')
      expect(text).not.toContain('Hinweistext')
      // betroffene Komponente im Vorfall trägt den übersetzten Namen
      expect(currentCards(wrapper)[0].text()).toContain('API (EN)')
    })

    it('lässt nicht übersetzte Texte im Original, ohne Fehlermeldung', async () => {
      const { wrapper } = await enMount()
      // „Web“, „CDN“ und das Wartungsfenster haben keine Übersetzung
      const names = wrapper.findAll('.component-tree-node-stub').map(n => n.text())
      expect(names).toEqual(expect.arrayContaining(['Web']))
      expect(wrapper.text()).toContain('Geplantes Upgrade')
      expect(wrapper.find('.sp-alert').exists()).toBe(false)
    })

    it('zeigt ohne Übersetzungen (leere Antwort) alles im Original', async () => {
      const { wrapper } = await enMount({ translations: trFixture({ components: {}, incidents: {}, notices: {} }) })
      expect(wrapper.text()).toContain('API down')
      expect(wrapper.text()).toContain('Hinweistext')
    })

    it('zeigt bei Fehler der Übersetzungs-Abfrage die Originale, ohne Fehlermeldung', async () => {
      const { wrapper } = await enMount({ translations: () => new Error('boom') })
      expect(wrapper.text()).toContain('API down')
      expect(wrapper.find('.sp-alert').exists()).toBe(false)
    })

    it('ignoriert eine Antwort in unerwarteter Form', async () => {
      const { wrapper } = await enMount({ translations: { components: [], incidents: 'x', notices: null } })
      expect(wrapper.text()).toContain('API down')
    })

    it('fragt bei 400 (Sprache bei der Seite nicht aktiv) ohne lang erneut ab', async () => {
      i18n.global.locale.value = 'en'
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('lang=en')) return jsonResponse({ code: 'invalid_request' }, 400)
        if (url.includes('/translations')) return jsonResponse(trFixture({ language: 'de' }))
        if (url.includes('/availability')) return jsonResponse({}, 404)
        return jsonResponse(fixtureWithNotice())
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(translationCalls(fetchMock)).toEqual(['/api/public/pages/acme/translations?lang=en', '/api/public/pages/acme/translations'])
      expect(wrapper.text()).toContain('API is down')
    })

    it('zeigt vor dem ersten Rendern keinen Originaltext, der danach umspringt (kein Layoutsprung)', async () => {
      i18n.global.locale.value = 'en'
      let release
      const gate = new Promise((r) => { release = r })
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('/translations')) { await gate; return jsonResponse(trFixture()) }
        if (url.includes('/availability')) return jsonResponse({}, 404)
        return jsonResponse(fixtureWithNotice())
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(wrapper.find('.loading-state').exists()).toBe(true)
      expect(wrapper.text()).not.toContain('API down')
      release()
      await flushPromises()
      expect(wrapper.find('.loading-state').exists()).toBe(false)
      expect(wrapper.text()).toContain('API is down')
      expect(wrapper.text()).not.toContain('API down\n')
    })

    it('wechselt beim Sprachwechsel die Texte und behält bis zur Antwort die bisherigen', async () => {
      i18n.global.locale.value = 'de'
      let release
      const gate = new Promise((r) => { release = r })
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('lang=en')) { await gate; return jsonResponse(trFixture()) }
        if (url.includes('/translations')) return jsonResponse(trFixture({ language: 'de', incidents: { 100: { title: 'API gestört', text: 'Keine Antwort' } } }))
        if (url.includes('/availability')) return jsonResponse({}, 404)
        return jsonResponse(fixtureWithNotice())
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(wrapper.text()).toContain('API gestört')
      i18n.global.locale.value = 'en'
      await flushPromises()
      expect(wrapper.text()).toContain('API gestört')
      release()
      await flushPromises()
      expect(wrapper.text()).toContain('API is down')
      expect(wrapper.text()).not.toContain('API gestört')
    })

    it('verwirft eine veraltete Antwort nach schnellem Sprachwechsel', async () => {
      i18n.global.locale.value = 'de'
      let releaseEn
      const gateEn = new Promise((r) => { releaseEn = r })
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('lang=en')) { await gateEn; return jsonResponse(trFixture()) }
        if (url.includes('lang=fr')) return jsonResponse(trFixture({ language: 'fr', incidents: { 100: { title: 'API en panne', text: 'x' } } }))
        if (url.includes('/translations')) return jsonResponse(trFixture({ language: 'de', incidents: {} }))
        if (url.includes('/availability')) return jsonResponse({}, 404)
        return jsonResponse(fixtureWithNotice())
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      i18n.global.locale.value = 'en'
      await flushPromises()
      i18n.global.locale.value = 'fr'
      await flushPromises()
      expect(wrapper.text()).toContain('API en panne')
      releaseEn()
      await flushPromises()
      expect(wrapper.text()).toContain('API en panne')
      expect(wrapper.text()).not.toContain('API is down')
      i18n.global.locale.value = 'de'
    })

    it('sucht und filtert auf den angezeigten (übersetzten) Texten', async () => {
      const { wrapper } = await enMount()
      vi.useFakeTimers({ shouldAdvanceTime: true })
      const input = wrapper.find('.search-input')
      await input.setValue('does not respond')
      await vi.advanceTimersByTimeAsync(300)
      expect(wrapper.text()).toContain('API is down')
      await input.setValue('antwortet nicht')
      await vi.advanceTimersByTimeAsync(300)
      expect(wrapper.text()).not.toContain('API is down')
      await input.setValue('(EN)')
      await vi.advanceTimersByTimeAsync(300)
      expect(wrapper.findAll('.component-tree-node-stub').map(n => n.text())).toEqual(['API (EN)'])
    })

    it('zeigt den Verlauf-Dialog mit übersetztem Titel und übersetzter Erstmeldung', async () => {
      const { wrapper } = await enMount()
      await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
      await flushPromises()
      expect(document.body.querySelector('#history-title').textContent).toBe('API is down')
      expect(document.body.querySelector('.history-modal').textContent).toContain('API does not respond')
      expect(document.body.querySelector('.history-modal').textContent).not.toContain('API antwortet nicht')
    })

    it('rendert Übersetzungen nie als HTML', async () => {
      const evil = '<img src=x onerror=alert(1)><b>fett</b>'
      const { wrapper } = await enMount({ translations: trFixture({ incidents: { 100: { title: evil, text: evil } } }) })
      expect(wrapper.html()).not.toContain('<img src="x"')
      expect(wrapper.find('.sp-incident img').exists()).toBe(false)
      expect(wrapper.find('.sp-incident b').exists()).toBe(false)
      expect(wrapper.text()).toContain('<b>fett</b>')
    })

    it('übersetzt externe Meldungen nicht', async () => {
      const dash = fixtureWithNotice()
      dash.external_incidents = [{
        id: 1, title: 'Externe Störung', status: 'investigating', kind: 'incident', started_at: '2026-10-29T10:00:00Z',
        updated_at: '2026-10-29T10:00:00Z', affected_components: [], updates: [], source: { name: 'Example Cloud', url: 'https://x.example' },
      }]
      const { wrapper } = await enMount({ dashboard: dash, translations: trFixture({ incidents: { 1: { title: 'External outage', text: 'x' } } }) })
      expect(wrapper.text()).toContain('Externe Störung')
      expect(wrapper.text()).not.toContain('External outage')
    })

    it('behält das Original, wenn ein Übersetzungstext leer ist', async () => {
      const { wrapper } = await enMount({ translations: trFixture({ incidents: { 100: { title: '  ', text: '' } } }) })
      expect(wrapper.text()).toContain('API down')
      expect(wrapper.text()).toContain('API antwortet nicht')
    })

    it('kommt mit einem Dashboard ohne notices und external_incidents zurecht', async () => {
      const dash = fixtureWithNotice()
      delete dash.notices
      const { wrapper } = await enMount({ dashboard: dash })
      expect(wrapper.text()).toContain('API is down')
      expect(wrapper.find('.notice-message').exists()).toBe(false)
    })

    it('verwirft die Übersetzungen eines Ladevorgangs, wenn währenddessen die Sprache wechselt', async () => {
      i18n.global.locale.value = 'de'
      let releaseDe
      const gate = new Promise((r) => { releaseDe = r })
      let first = true
      const fetchMock = vi.fn(async (url) => {
        if (url.includes('lang=de') && first) { first = false; await gate; return jsonResponse(trFixture({ language: 'de', incidents: { 100: { title: 'ALT DE', text: 'x' } } })) }
        if (url.includes('lang=en')) return jsonResponse(trFixture())
        if (url.includes('/availability')) return jsonResponse({}, 404)
        return jsonResponse(fixtureWithNotice())
      })
      const mounting = mountStatusPage({ fetchImpl: fetchMock })
      await flushPromises()
      i18n.global.locale.value = 'en'
      await flushPromises()
      releaseDe()
      const { wrapper } = await mounting
      await flushPromises()
      expect(wrapper.text()).toContain('API is down')
      expect(wrapper.text()).not.toContain('ALT DE')
    })

    it('hat keine axe-Funde mit übersetzten Texten', async () => {
      const { wrapper } = await enMount()
      expect(await axe(wrapper)).toHaveNoViolations()
    })
  })
  // Updates eines Vorfalls in der Sprache des Besuchers.
  describe('Update-Übersetzungen', () => {
    const upd = (id, message) => ({ id, status: 'monitoring', message, created_at: `2026-07-01T1${id}:00:00Z` })
    const detail = { updates: [upd(1, 'Erstes Update'), upd(2, 'Zweites Update')] }
    const updateTrCalls = (fetchMock) => fetchMock.mock.calls.map(([url]) => url).filter(url => url.includes('/update-translations'))
    const withUpdateTranslations = ({ texts = (lang) => (lang === 'en' ? { 1: 'First update', 2: 'Second update' } : {}), failing = false } = {}) => vi.fn(async (url) => {
      if (url.includes('/update-translations')) {
        if (failing) throw new Error('boom')
        const lang = /lang=([a-z-]+)/.exec(url)?.[1]
        return jsonResponse({ language: lang || 'de', default_language: 'de', updates: texts(lang) })
      }
      if (url.includes('/translations')) return jsonResponse({ language: 'de', default_language: 'de', components: {}, incidents: {}, notices: {} })
      if (url.includes('/availability')) return jsonResponse({}, 404)
      if (url.includes('/incidents/')) return jsonResponse(detail)
      return jsonResponse(dashboardFixture())
    })
    const timelineTexts = (wrapper) => currentCards(wrapper)[0].findAll('.sp-timeline-message').map(m => m.text())

    it('zeigt die Updates der Zeitleiste in der Sprache des Besuchers und ruft sie mit Sprache und Token ab', async () => {
      i18n.global.locale.value = 'en'
      const fetchMock = withUpdateTranslations()
      const { wrapper } = await mountStatusPage({ routePath: '/status/acme?token=tok1', fetchImpl: fetchMock })
      expect(timelineTexts(wrapper)).toEqual(['Second update', 'First update'])
      expect(updateTrCalls(fetchMock)).toEqual(['/api/public/pages/acme/incidents/100/update-translations?lang=en&token=tok1'])
    })

    it('zeigt ohne Übersetzung das Original, auch pro Update', async () => {
      i18n.global.locale.value = 'en'
      const { wrapper } = await mountStatusPage({ fetchImpl: withUpdateTranslations({ texts: () => ({ 2: 'Second update', 1: '  ' }) }) })
      expect(timelineTexts(wrapper)).toEqual(['Second update', 'Erstes Update'])
    })

    it('zeigt das Original, wenn der Abruf der Update-Texte scheitert', async () => {
      i18n.global.locale.value = 'en'
      const { wrapper } = await mountStatusPage({ fetchImpl: withUpdateTranslations({ failing: true }) })
      expect(timelineTexts(wrapper)).toEqual(['Zweites Update', 'Erstes Update'])
    })

    it('übersetzt die Updates auch im Verlauf-Dialog, die Erstmeldung bleibt davon unberührt', async () => {
      i18n.global.locale.value = 'en'
      const { wrapper } = await mountStatusPage({ fetchImpl: withUpdateTranslations() })
      await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
      await flushPromises()
      const msgs = [...document.body.querySelectorAll('.tl-msg')].map(m => m.textContent)
      expect(msgs).toEqual(['Second update', 'First update', 'API antwortet nicht']) // Erstmeldung zuletzt, unübersetzt
    })

    it('holt beim Sprachwechsel die Texte der geladenen Vorfälle neu', async () => {
      i18n.global.locale.value = 'de'
      const fetchMock = withUpdateTranslations()
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      expect(timelineTexts(wrapper)).toEqual(['Zweites Update', 'Erstes Update'])
      i18n.global.locale.value = 'en'
      await flushPromises()
      expect(timelineTexts(wrapper)).toEqual(['Second update', 'First update'])
      expect(updateTrCalls(fetchMock)).toHaveLength(2)
      i18n.global.locale.value = 'de'
      await flushPromises()
      expect(timelineTexts(wrapper)).toEqual(['Zweites Update', 'Erstes Update'])
    })

    it('holt beim Sprachwechsel auch die Texte des offenen Verlauf-Dialogs neu', async () => {
      i18n.global.locale.value = 'de'
      const fetchMock = withUpdateTranslations()
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
      await flushPromises()
      const msgs = () => [...document.body.querySelectorAll('.tl-msg')].map(m => m.textContent)
      expect(msgs()).toEqual(['Zweites Update', 'Erstes Update', 'API antwortet nicht'])
      i18n.global.locale.value = 'en'
      await flushPromises()
      expect(msgs()).toEqual(['Second update', 'First update', 'API antwortet nicht'])
      i18n.global.locale.value = 'de'
    })

    it('holt beim Sprachwechsel mit offenem Verlauf-Dialog eines inline geladenen Vorfalls genau einmal', async () => {
      i18n.global.locale.value = 'de'
      const fetchMock = withUpdateTranslations()
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      await currentCards(wrapper)[0].find('.incident-history-btn').trigger('click')
      await flushPromises()
      const before = updateTrCalls(fetchMock).length
      i18n.global.locale.value = 'en'
      await flushPromises()
      expect(updateTrCalls(fetchMock).length - before).toBe(1)
      i18n.global.locale.value = 'de'
    })

    it('verwirft eine veraltete Antwort nach schnellem Sprachwechsel', async () => {
      i18n.global.locale.value = 'de'
      let releaseEn
      const gate = new Promise((r) => { releaseEn = r })
      const base = withUpdateTranslations({ texts: (lang) => ({ 1: `Text ${lang}` }) })
      const fetchMock = vi.fn(async (url, o) => {
        if (url.includes('/update-translations') && url.includes('lang=en')) { await gate }
        return base(url, o)
      })
      const { wrapper } = await mountStatusPage({ fetchImpl: fetchMock })
      i18n.global.locale.value = 'en'
      await flushPromises()
      i18n.global.locale.value = 'fr'
      await flushPromises()
      expect(timelineTexts(wrapper)).toContain('Text fr')
      releaseEn()
      await flushPromises()
      expect(timelineTexts(wrapper)).toContain('Text fr')
      expect(timelineTexts(wrapper)).not.toContain('Text en')
      i18n.global.locale.value = 'de'
    })
  })
})
