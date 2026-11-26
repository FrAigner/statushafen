import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { registerLocale, SUPPORTED_LOCALES } from '../../i18n'
import { useContentLanguages } from '../../composables/useContentLanguages'
import AdminUiTranslations from './AdminUiTranslations.vue'
import { axe } from '../../test-support/axe'
import { stubConfirm } from '../../test-support/confirm'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

let activeWrapper = null

async function mountView(fetchImpl) {
  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse({}))
  const wrapper = mount(AdminUiTranslations, { global: { plugins: [i18n] } })
  await flushPromises()
  activeWrapper = wrapper
  return wrapper
}

// Narrows the 700+ app-wide keys down to a manageable, stable subset
// - the namespace filter only knows top-level namespaces (e.g. "adminNav");
// "adminNav.items.sso" is additionally narrowed via the free-text search.
async function filterToAdminNavItems(wrapper) {
  await wrapper.find('select[aria-label="Bereich"]').setValue('adminNav')
  await wrapper.find('.comp-search input').setValue('sso')
}

describe('AdminUiTranslations', () => {
  // The filters' aria-labels were hard-coded English ("Scope", "Language", "Namespace").
  it('übersetzt die aria-labels der Filter-Selects (de und en)', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView()
    const labels = wrapper.findAll('select.comp-filter').map(s => s.attributes('aria-label'))
    expect(labels).toEqual(['Geltungsbereich', 'Sprache', 'Bereich'])
    for (const k of ['ariaScope', 'ariaLanguage', 'ariaNamespace']) {
      expect(i18n.global.t(`uiTranslations.${k}`, {}, { locale: 'en' })).not.toBe(`uiTranslations.${k}`)
    }
    wrapper.unmount()
  })

  // The language selection showed the raw codes "de"/"en" instead of the language names.
  it('Sprachauswahl zeigt die Sprachnamen, der Wert bleibt der Code (nur de und en)', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView()
    const options = wrapper.findAll('select[aria-label="Sprache"] option')
    expect(options.map(o => o.attributes('value'))).toEqual(['de', 'en'])
    expect(options.map(o => o.text())).toEqual([
      i18n.global.t('language.de'),
      i18n.global.t('language.en')
    ])
    expect(options.map(o => o.text())).not.toContain('de')
    expect(options.map(o => o.text())).not.toContain('en')
    wrapper.unmount()
  })

  // The filter selects used .form-select (44 px high, 100 % wide, own look)
  // and looked foreign next to the search bar; the shared pattern of the admin card headers is .comp-filter.
  it('Filter-Selects im Kartenkopf nutzen das gemeinsame Muster .comp-filter', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView()
    const selects = wrapper.findAll('.comp-card__tools select')
    expect(selects).toHaveLength(3)
    for (const sel of selects) {
      expect(sel.classes()).toContain('comp-filter')
      expect(sel.classes()).not.toContain('form-select')
      expect(sel.classes()).not.toContain('filter-select')
    }
    wrapper.unmount()
  })

  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('Global Admin sieht den Scope-Umschalter und lädt globale Overrides', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = await mountView(fetchMock)

    expect(wrapper.find('select[aria-label="Geltungsbereich"]').exists()).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith('/api/admin/ui-overrides?language_code=de', expect.anything())
  })

  it('lädt beim Wechsel von Sprache und Bereich die passenden Overrides neu', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    useTenant().setActiveTenant('1')
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = await mountView(fetchMock)

    await wrapper.find('select[aria-label="Sprache"]').setValue('en')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/admin/ui-overrides?language_code=en', expect.anything())

    await wrapper.find('select[aria-label="Geltungsbereich"]').setValue('page')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/ui-overrides?language_code=en', expect.anything())
  })

  it('zeigt den Default-Wert eines bekannten Keys an', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView()
    await filterToAdminNavItems(wrapper)

    expect(wrapper.text()).toContain('adminNav.items.sso')
    expect(wrapper.text()).toContain('SSO Konfiguration')
  })

  it('filtert per Suche nach Key oder Text', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView()
    await wrapper.find('select[aria-label="Bereich"]').setValue('adminNav')
    const rowsBefore = wrapper.findAll('.override-row').length

    await wrapper.find('.comp-search input').setValue('sso')
    const rowsAfter = wrapper.findAll('.override-row')

    expect(rowsAfter.length).toBeLessThan(rowsBefore)
    expect(rowsAfter.length).toBeGreaterThan(0)
    expect(wrapper.text()).toContain('adminNav.items.sso')
  })

  it('Speichern legt einen Override per PUT an und zeigt danach den Zurücksetzen-Button', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = await mountView(fetchMock)
    await filterToAdminNavItems(wrapper)

    const row = wrapper.find('.override-row')
    await row.find('.override-input').setValue('Mein SSO')
    await row.find('.action-btn').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/admin/ui-overrides', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ language_code: 'de', key: 'adminNav.items.sso', value: 'Mein SSO' }),
    }))
    expect(row.findAll('.action-btn')).toHaveLength(2)
  })

  // Saving an empty row stored "" as a (global) override
  // and blanked the text for all visitors.
  it('Speichern ist bei leerem oder nur aus Leerzeichen bestehendem Feld gesperrt und sendet kein PUT', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = await mountView(fetchMock)
    await filterToAdminNavItems(wrapper)

    const row = wrapper.find('.override-row')
    const saveButton = () => row.findAll('.action-btn')[0]
    expect(saveButton().attributes('disabled')).toBeDefined()
    expect(saveButton().attributes('title')).toBe(i18n.global.t('uiTranslations.saveEmptyHint'))
    await saveButton().trigger('click')

    await row.find('.override-input').setValue('Mein SSO')
    expect(saveButton().attributes('disabled')).toBeUndefined()
    expect(saveButton().attributes('title')).toBeUndefined()

    for (const value of ['', '   ']) {
      await row.find('.override-input').setValue(value)
      expect(saveButton().attributes('disabled')).toBeDefined()
      await saveButton().trigger('click')
    }
    await flushPromises()

    expect(fetchMock.mock.calls.some(([, opts]) => opts?.method === 'PUT')).toBe(false)
  })

  it('reports a failed load of the overrides with the server message', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = await mountView(vi.fn().mockResolvedValue(jsonResponse({ error: 'kaputt' }, 500)))

    expect(wrapper.text()).toContain('UI-Texte konnten nicht geladen werden: kaputt')
  })

  it('reports failed save and reset of a row with the server message', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    stubConfirm(true)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn(async (url, options = {}) => (options.method
      ? jsonResponse({ error: 'abgelehnt' }, 500)
      : jsonResponse({ 'adminNav.items.sso': 'Mein SSO' })))
    const wrapper = await mountView(fetchMock)
    await filterToAdminNavItems(wrapper)

    const row = wrapper.find('.override-row')
    await row.find('.override-input').setValue('Anderes SSO')
    await row.findAll('.action-btn')[0].trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Speichern fehlgeschlagen: abgelehnt')

    await row.findAll('.action-btn')[1].trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Zurücksetzen fehlgeschlagen: abgelehnt')
  })

  it('Zurücksetzen fragt mit Key, Bereich und Sprache nach und löscht bei Abbruch nichts', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const confirm = stubConfirm(false)
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Mein SSO' }))
    const wrapper = await mountView(fetchMock)
    await filterToAdminNavItems(wrapper)

    const row = wrapper.find('.override-row')
    await row.findAll('.action-btn')[1].trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith(i18n.global.t('uiTranslations.confirmReset', {
      key: 'adminNav.items.sso', scope: i18n.global.t('uiTranslations.scopeGlobal'), language: 'de',
    }))
    expect(fetchMock.mock.calls.some(([, opts]) => opts?.method === 'DELETE')).toBe(false)
    expect(row.findAll('.action-btn')).toHaveLength(2)
  })

  it('nennt im Seiten-Scope die Seite als Bereich der Bestätigung', async () => {
    useTenant().setActiveTenant('7')
    setAuthAsPageRole('jwt-1', { is_global_admin: false, permissions: 'ui_translations:update' })
    const confirm = stubConfirm(true)
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Seiten-SSO' }))
    const wrapper = await mountView(fetchMock)
    await filterToAdminNavItems(wrapper)

    await wrapper.find('.override-row').findAll('.action-btn')[1].trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith(expect.stringContaining(i18n.global.t('uiTranslations.scopePage')))
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/pages/7/ui-overrides?language_code=de&key=adminNav.items.sso',
      expect.objectContaining({ method: 'DELETE' })
    )
  })

  it('Zurücksetzen löscht den Override nach Bestätigung per DELETE', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    stubConfirm(true)
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Mein SSO' }))
    const wrapper = await mountView(fetchMock)
    await filterToAdminNavItems(wrapper)

    const row = wrapper.find('.override-row')
    const buttons = row.findAll('.action-btn')
    expect(buttons).toHaveLength(2)
    await buttons[1].trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/admin/ui-overrides?language_code=de&key=adminNav.items.sso',
      expect.objectContaining({ method: 'DELETE' })
    )
    expect(row.findAll('.action-btn')).toHaveLength(1)
  })

  it('Page Admin ohne Global-Admin-Rechte sieht keinen Scope-Umschalter und editiert die Overrides seiner aktiven Seite', async () => {
    useTenant().setActiveTenant('7')
    setAuthAsPageRole('jwt-1', { is_global_admin: false, permissions: 'ui_translations:update' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = await mountView(fetchMock)

    expect(wrapper.find('select[aria-label="Geltungsbereich"]').exists()).toBe(false)
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/7/ui-overrides?language_code=de', expect.anything())
  })

  it('Ohne ui_translations:update-Berechtigung sind die Overrides nur lesbar (keine Aktionen)', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: false, permissions: '' })
    useTenant().setActiveTenant('7')
    const wrapper = await mountView(vi.fn().mockResolvedValue(jsonResponse({})))
    await filterToAdminNavItems(wrapper)

    expect(wrapper.find('.override-input').exists()).toBe(false)
    expect(wrapper.find('.override-row .action-btn').exists()).toBe(false)
  })

  it('zeigt einen Hinweis, wenn im Seiten-Scope keine aktive Seite gewählt ist', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: false, permissions: 'ui_translations:update' })
    useTenant().setActiveTenant(null)
    const wrapper = await mountView()

    // Stage 4d: title as h2 (the top bar carries the h1), empty state with an icon.
    expect(wrapper.find('h2.page-head__title').exists()).toBe(true)
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(true)

    expect(wrapper.text()).toContain('Bitte zuerst eine Statusseite auswählen.')
  })

  it('zeigt bei einer Suche ohne Treffer den Leerzustand in der Tabelle', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView()
    await wrapper.find('.comp-search input').setValue('gibt-es-garantiert-nicht-xyz')

    expect(wrapper.findAll('.override-row')).toHaveLength(0)
    expect(wrapper.find('.empty-state').text()).toBe(i18n.global.t('uiTranslations.empty'))
  })

  it('hat keine axe-Funde in der gefilterten Liste', async () => {
    const wrapper = await mountView()
    await filterToAdminNavItems(wrapper)
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Direction C: breadcrumbs (global -> administration, page scope -> page name),
  // card with filters in the header, "Customised" status with icon and text.
  it('zeigt Brotkrumen je Bereich, eine Karte mit Filtern im Kopf und markiert angepasste Texte', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    useTenant().setActiveTenant('1')
    useTenant().setMyPages([{ ID: 1, Name: 'Acme' }])
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Single Sign-on' }))
    const wrapper = await mountView(fetchMock)

    expect(wrapper.findAll('.page-head__crumbs li').map(li => li.text())).toEqual(['Administration', 'UI-Texte'])
    await wrapper.find('select[aria-label="Geltungsbereich"]').setValue('page')
    await flushPromises()
    expect(wrapper.findAll('.page-head__crumbs li').map(li => li.text())).toEqual(['Acme', 'UI-Texte'])

    expect(wrapper.find('section.comp-card h3').text()).toBe('Alle Texte')
    expect(wrapper.findAll('.comp-card__head select')).toHaveLength(3)
    expect(wrapper.find('.comp-card__head input[type="search"]').exists()).toBe(true)

    await filterToAdminNavItems(wrapper)
    const row = wrapper.findAll('tbody tr').find(r => r.text().includes('adminNav.items.sso'))
    expect(row.find('.st-badge').text()).toBe('Angepasst')
    expect(wrapper.findAll('tbody tr').filter(r => r.find('.st-badge').exists())).toHaveLength(1)
  })
})


// Part 1: the language selection only offered de/en; now all active languages
// of the catalog (global) or of the selected page, with their own name.
describe('AdminUiTranslations: Sprachauswahl mit allen aktiven Sprachen', () => {
  const CATALOG = [
    { Code: 'de', Name: 'Deutsch', IsActive: true },
    { Code: 'en', Name: 'English', IsActive: true },
    { Code: 'fr', Name: 'Français', IsActive: true },
    { Code: 'hr', Name: 'Hrvatski', IsActive: true },
    { Code: 'es', Name: 'Español', IsActive: false },
  ]
  const PAGE_LANGS = [
    { LanguageCode: 'de', IsDefault: true },
    { LanguageCode: 'hr', IsDefault: false },
  ]
  const apiMock = (overrides = {}) => vi.fn().mockImplementation(async (url) => {
    if (url === '/api/languages') return jsonResponse(overrides.catalog ?? CATALOG)
    if (/\/api\/pages\/\d+\/languages$/.test(url)) return jsonResponse(overrides.pageLanguages ?? PAGE_LANGS)
    return jsonResponse({})
  })
  const options = (wrapper) => wrapper.findAll('select[aria-label="Sprache"] option')

  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    useContentLanguages().catalog.value = []
    useContentLanguages().pageLanguages.value = []
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('Global: bietet alle aktiven Sprachen des Katalogs mit Eigenbezeichnung an (inaktive nicht)', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView(apiMock())
    expect(options(wrapper).map(o => o.attributes('value'))).toEqual(['de', 'en', 'fr', 'hr'])
    expect(options(wrapper).map(o => o.text())).toEqual(['Deutsch', 'English', 'Français', 'Hrvatski'])
  })

  it('Seite: bietet die Sprachen der gewählten Seite an, zusätzlich zu de und en', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    useTenant().setActiveTenant('7')
    const fetchMock = apiMock()
    const wrapper = await mountView(fetchMock)
    await wrapper.find('select[aria-label="Geltungsbereich"]').setValue('page')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/7/languages', expect.anything())
    expect(options(wrapper).map(o => o.attributes('value'))).toEqual(['de', 'en', 'hr'])
  })

  it('Sprache ohne Standardtexte: Hinweis, Standardspalte zeigt den englischen Text, Overrides unter dem Code', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const fetchMock = apiMock()
    const wrapper = await mountView(fetchMock)
    expect(wrapper.text()).not.toContain('keine eigenen Standardtexte')
    await wrapper.find('select[aria-label="Sprache"]').setValue('hr')
    await flushPromises()
    expect(wrapper.text()).toContain('Für Hrvatski gibt es keine eigenen Standardtexte')
    expect(wrapper.text()).toContain('Rückfall Englisch')
    expect(fetchMock).toHaveBeenCalledWith('/api/admin/ui-overrides?language_code=hr', expect.anything())
    await wrapper.find('select[aria-label="Bereich"]').setValue('adminNav')
    await wrapper.find('.comp-search input').setValue('sso')
    expect(wrapper.text()).toContain(i18n.global.t('adminNav.items.sso', {}, { locale: 'en' }))
  })

  it('wechselt auf Deutsch, wenn die gewählte Sprache im anderen Bereich nicht angeboten wird', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    useTenant().setActiveTenant('7')
    const wrapper = await mountView(apiMock())
    await wrapper.find('select[aria-label="Sprache"]').setValue('fr')
    await wrapper.find('select[aria-label="Geltungsbereich"]').setValue('page')
    await flushPromises()
    expect(wrapper.find('select[aria-label="Sprache"]').element.value).toBe('de')
  })

  it('bleibt bei fehlgeschlagener Sprachliste bei de und en', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const fetchMock = vi.fn().mockImplementation(async (url) =>
      url === '/api/languages' ? jsonResponse({ error: 'x' }, 500) : jsonResponse({}))
    const wrapper = await mountView(fetchMock)
    expect(options(wrapper).map(o => o.attributes('value'))).toEqual(['de', 'en'])
  })

  it('zeigt Seitensprachen ohne Katalogeintrag mit dem Code', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: false, permissions: 'ui_translations:update' })
    useTenant().setActiveTenant('7')
    const wrapper = await mountView(apiMock({ catalog: [], pageLanguages: [{ LanguageCode: 'xx' }, { LanguageCode: 'xx' }] }))
    expect(options(wrapper).map(o => o.text())).toEqual(['Deutsch', 'Englisch', 'xx'])
  })

  it('Sprache, deren Datei sich nicht laden lässt: Hinweis bleibt, Standard ist Englisch', async () => {
    registerLocale('zv', async () => { throw new Error('offline') })
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView(apiMock({ catalog: [...CATALOG, { Code: 'zv', Name: 'Viertsprache', IsActive: true }] }))
    await wrapper.find('select[aria-label="Sprache"]').setValue('zv')
    await flushPromises()
    expect(wrapper.text()).toContain('Für Viertsprache gibt es keine eigenen Standardtexte')
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zv'), 1)
  })

  it('Sprache mit nachladbarer Datei: eigene Standardtexte, kein Rückfall-Hinweis', async () => {
    registerLocale('zw', async () => ({ default: { adminNav: { items: { sso: 'ZW-SSO-Text' } } } }))
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const wrapper = await mountView(apiMock({ catalog: [...CATALOG, { Code: 'zw', Name: 'Zweitsprache', IsActive: true }] }))
    await wrapper.find('select[aria-label="Sprache"]').setValue('zw')
    await flushPromises()
    expect(wrapper.text()).not.toContain('keine eigenen Standardtexte')
    await wrapper.find('select[aria-label="Bereich"]').setValue('adminNav')
    expect(wrapper.text()).toContain('ZW-SSO-Text')
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zw'), 1)
  })
})
