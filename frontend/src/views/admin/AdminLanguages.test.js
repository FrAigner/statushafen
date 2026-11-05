import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminLanguages from './AdminLanguages.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'

// Stage 1 von 5 der Mehrsprachigkeit: Component-Tests für die
// Sprachverwaltung - globaler Katalog (global-admin-only CRUD) + pro
// Statusseite aktivierte Sprachen inkl. Default.

function catalogFixture() {
  return [
    { ID: 1, Code: 'de', Name: 'Deutsch', IsActive: true },
    { ID: 2, Code: 'en', Name: 'English', IsActive: true },
  ]
}
function pageLanguagesFixture() {
  return [
    { ID: 10, LanguageCode: 'de', IsDefault: true },
    { ID: 11, LanguageCode: 'en', IsDefault: false },
  ]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ catalogOverride, pageLanguagesOverride, writeOk = true, deleteResponse } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'DELETE' && /\/api\/languages\/\d+$/.test(url)) {
      return deleteResponse ?? { ok: true, status: 204, json: async () => ({}) }
    }
    if (method === 'GET' && /\/api\/languages$/.test(url)) {
      return jsonResponse(catalogOverride ?? catalogFixture())
    }
    if (method === 'GET' && /\/languages$/.test(url)) {
      return jsonResponse(pageLanguagesOverride ?? pageLanguagesFixture())
    }
    if (method === 'PUT' && /\/languages$/.test(url)) {
      return writeOk ? jsonResponse(pageLanguagesFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'POST' && /\/api\/languages$/.test(url)) {
      return writeOk ? jsonResponse({ ID: 3, Code: 'fr', Name: 'Français', IsActive: true }) : jsonResponse({ error: 'kaputt' }, 409)
    }
    if (method === 'PUT' && /\/api\/languages\/\d+$/.test(url)) {
      return writeOk ? jsonResponse({ ID: 1, Code: 'de', Name: 'Deutsch (updated)', IsActive: true }) : jsonResponse({ error: 'kaputt' }, 500)
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let activeWrapper = null

// Zeilenaktionen stehen im Zeilenmenü (RowMenu), die Einträge sind auch geschlossen im DOM.
const menuItem = (row, text) => row.findAll('[role="menuitem"]').find(i => i.text().includes(text))

// Seitensprachen speichern verlangt languages:update auf der aktiven
// Seite; ohne angemeldeten Nutzer meldet der Helfer einen Seitennutzer mit
// pagePerms an (Standard: darf speichern).
async function mountAdminLanguages({ fetchImpl, tenantId = '1', pagePerms = 'languages:update' } = {}) {
  if (!useAuth().currentUser.value) useAuth().setAuth('jwt-1', { permissions: '' })
  useTenant().setMyPages([{ ID: Number(tenantId), Permissions: pagePerms }])
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminLanguages, {
    global: { plugins: [i18n], stubs: onyxFormStubs },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminLanguages', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // Tests assertieren auf den deutschen Wortlaut übersetzter Texte;
    // ohne gespeicherte Präferenz würde detectInitialLocale() in der Testumgebung
    // (happy-dom navigator.language === 'en-US') sonst auf 'en' fallen.
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('lädt Katalog + Seiten-Sprachen und rendert beide Tabellen', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })

    expect(fetchMock).toHaveBeenCalledWith('/api/languages', expect.anything())
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/languages', expect.anything())
    expect(wrapper.text()).toContain('Deutsch')
    expect(wrapper.text()).toContain('English')

    const checkboxes = wrapper.findAll('input[type="checkbox"][value]')
    expect(checkboxes.length).toBe(2)
    expect(checkboxes[0].element.checked).toBe(true)
    expect(checkboxes[1].element.checked).toBe(true)
  })

  // Betreiber-Wunsch 31.10.: Flagge vor dem Code, Spalte „Sprache“ in der Oberflächensprache.
  it('zeigt Flagge (aria-hidden) und den Sprachnamen in der Oberflächensprache; unbekannter Code ohne Flagge', async () => {
    const catalogOverride = [...catalogFixture(), { ID: 9, Code: 'x1', Name: 'Test', IsActive: true }]
    const { wrapper } = await mountAdminLanguages({ fetchImpl: makeFetchRouter({ catalogOverride }) })

    const heads = wrapper.findAll('thead th').map(h => h.text())
    expect(heads.indexOf('Sprache')).toBe(heads.indexOf('Name') - 1)
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].findAll('td')[1].text()).toBe('Deutsch')
    expect(rows[1].findAll('td')[1].text()).toBe('Englisch')
    const flag = rows[0].find('.lang-flag')
    expect(flag.text()).toBe('\u{1F1E9}\u{1F1EA}')
    expect(flag.attributes('aria-hidden')).toBe('true')
    expect(rows[2].find('.lang-flag').exists()).toBe(false)
    expect(rows[2].findAll('td')[1].text()).toBe('x1')
    // Auswahl der Seitensprachen: Flagge vor dem Namen.
    expect(wrapper.find('.stack-list .lang-flag').text()).toBe('\u{1F1E9}\u{1F1EA}')

    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(wrapper.findAll('thead th').map(h => h.text())).toContain('Language')
    expect(wrapper.findAll('tbody tr')[1].findAll('td')[1].text()).toBe('English')
    expect(wrapper.findAll('tbody tr')[0].findAll('td')[1].text()).toBe('German')
  })

  it('blendet den "+ Neue Sprache"-Button und die Aktionen-Spalte für Nicht-Global-Admins aus', async () => {
    const { wrapper } = await mountAdminLanguages()

    expect(wrapper.find('th.actions-col').exists()).toBe(false)
    expect(wrapper.find('[role="menuitem"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Neue Sprache')
  })

  it('zeigt Katalog-CRUD-Aktionen für Global Admins', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const { wrapper } = await mountAdminLanguages()

    expect(wrapper.find('th.actions-col').exists()).toBe(true)
    expect(wrapper.text()).toContain('Neue Sprache')
  })

  it('legt eine neue Sprache über das Formular an', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })

    await wrapper.find('button').trigger('click') // "+ Neue Sprache"
    await wrapper.find('#lang-code').setValue('fr')
    await wrapper.find('#lang-name').setValue('Français')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/languages',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ code: 'fr', name: 'Français' }) })
    )
    expect(wrapper.text()).not.toContain('Neue Sprache anlegen')
  })

  // Ein Viewer (kein languages:update) sah „Speichern“ und bekam
  // beim Klick 403. Jetzt sind die Checkboxen gesperrt und der Knopf fehlt.
  it('sperrt die Seitensprachen ohne languages:update und blendet Speichern aus', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock, pagePerms: 'components:read,incidents:read,notices:read' })

    const inputs = wrapper.findAll('input[type="checkbox"][value], input[type="radio"]')
    expect(inputs.length).toBeGreaterThan(0)
    for (const input of inputs) expect(input.element.disabled).toBe(true)
    expect(wrapper.findAll('button').some(b => b.text().includes('Speichern'))).toBe(false)
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'PUT')).toBe(false)
  })

  it('gibt die Seitensprachen mit languages:update frei', async () => {
    const { wrapper } = await mountAdminLanguages()

    const inputs = wrapper.findAll('input[type="checkbox"][value], input[type="radio"]')
    expect(inputs.length).toBeGreaterThan(0)
    for (const input of inputs) expect(input.element.disabled).toBe(false)
    expect(wrapper.findAll('button').some(b => b.text().includes('Speichern'))).toBe(true)
  })

  it('übernimmt die per Radio gewählte Standardsprache beim Speichern', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })

    await wrapper.find('input[type="radio"][value="en"]').setValue(true)
    await wrapper.findAll('button').find(b => b.text().includes('Speichern')).trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/pages/1/languages',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ codes: ['de', 'en'], default: 'en' }) })
    )
  })

  it('speichert die Sprachauswahl der Statusseite', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })

    const saveButtons = wrapper.findAll('button').filter(b => b.text().includes('Speichern'))
    await saveButtons[0].trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/pages/1/languages',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ codes: ['de', 'en'], default: 'de' }) })
    )
    expect(wrapper.text()).toContain('Sprachauswahl gespeichert')
  })

  it('zeigt eine Warnung, wenn alle Sprachen abgewählt werden', async () => {
    const { wrapper } = await mountAdminLanguages()

    const checkboxes = wrapper.findAll('input[type="checkbox"][value]')
    for (const cb of checkboxes) {
      await cb.setValue(false)
    }
    const saveButtons = wrapper.findAll('button').filter(b => b.text().includes('Speichern'))
    await saveButtons[0].trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Mindestens eine Sprache muss aktiviert sein')
  })

  it('zeigt keine Seiten-Sprachauswahl ohne aktive Statusseite', async () => {
    const { wrapper } = await mountAdminLanguages({ tenantId: null })

    expect(wrapper.text()).not.toContain('Sprachen dieser Statusseite')
  })

  // Laden -> Fehlerfall -> Speichern werden ausdrücklich per
  // flushPromises abgewartet, damit die Coverage nicht vom Timing abhängt.
  it('bearbeitet eine Sprache per PUT und bricht das Bearbeiten ab', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    expect(wrapper.find('form.edit-form').exists()).toBe(true)
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
    expect(wrapper.find('form.edit-form').exists()).toBe(false)

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await wrapper.find('#lang-name').setValue('Deutsch (updated)')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const put = fetchMock.mock.calls.find(([url, o]) => o?.method === 'PUT' && /\/api\/languages\/1$/.test(url))
    expect(JSON.parse(put[1].body)).toMatchObject({ name: 'Deutsch (updated)', is_active: true })
    expect(wrapper.find('form.edit-form').exists()).toBe(false)
  })

  // Löschen im Zeilenmenü mit Bestätigung; der Server entscheidet über 409.
  it('löscht eine Sprache nach Bestätigung per DELETE und lädt den Katalog neu', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const confirm = stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })
    const catalogLoads = () => fetchMock.mock.calls.filter(([u, o]) => u === '/api/languages' && !o?.method).length
    const before = catalogLoads()

    await menuItem(wrapper.findAll('tbody tr')[1], 'Löschen').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('English'))
    expect(fetchMock).toHaveBeenCalledWith('/api/languages/2', expect.objectContaining({ method: 'DELETE' }))
    expect(catalogLoads()).toBe(before + 1)
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('löscht nichts, wenn die Bestätigung abgelehnt wird', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    stubConfirm(false)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)
  })

  it('zeigt die Servermeldung, wenn die Sprache Standardsprache einer Seite ist (409)', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    stubConfirm(true)
    const deleteResponse = jsonResponse({ error: 'x', code: 'language_is_default', count: 1, pages: ['Acme'] }, 409)
    const { wrapper } = await mountAdminLanguages({ fetchImpl: makeFetchRouter({ deleteResponse }) })

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    const alert = wrapper.find('.alert-error')
    expect(alert.text()).toContain('Löschen fehlgeschlagen')
    expect(alert.text()).toContain('Standardsprache von 1 Statusseite(n) (Acme)')
    expect(wrapper.text()).toContain('Deutsch')
  })

  it('bietet Löschen nur Global Admins an', async () => {
    const { wrapper } = await mountAdminLanguages()
    expect(wrapper.text()).not.toContain('Löschen')
  })

  it('zeigt eine Fehlermeldung und lässt das Formular offen, wenn das Anlegen fehlschlägt', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const { wrapper } = await mountAdminLanguages({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await wrapper.find('button').trigger('click')
    await wrapper.find('#lang-code').setValue('fr')
    await wrapper.find('#lang-name').setValue('Français')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.alert-error').exists()).toBe(true)
    expect(wrapper.find('form.edit-form').exists()).toBe(true)
    await wrapper.find('.alert-close').trigger('click')
    expect(wrapper.find('.alert').exists()).toBe(false)
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern der Sprachauswahl fehlschlägt', async () => {
    const { wrapper } = await mountAdminLanguages({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    const saveButtons = wrapper.findAll('button').filter(b => b.text().includes('Speichern'))
    await saveButtons[0].trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500))
    const { wrapper } = await mountAdminLanguages({ fetchImpl })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  it('setzt die Standardsprache auf die erste verbleibende, wenn sie abgewählt wird', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })

    await wrapper.findAll('input[type="checkbox"][value]')[0].setValue(false)
    const saveButtons = wrapper.findAll('button').filter(b => b.text().includes('Speichern'))
    await saveButtons[0].trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/pages/1/languages',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ codes: ['en'], default: 'en' }) })
    )
  })

  // Richtung C: Kartenkopf, Untertitel mit Zählung, Suche, Fehler am Feld.
  it('zeigt Kartenkopf und Untertitel mit Zählung', async () => {
    const { wrapper } = await mountAdminLanguages()
    expect(wrapper.find('.comp-card__title').text()).toBe('Sprachkatalog')
    expect(wrapper.find('.page-head__subtitle').text()).toBe('2 Sprachen · 2 aktiv')
  })

  it('sucht nach Code oder Name; ohne Treffer erscheint der Leerzustand statt der Tabelle', async () => {
    const { wrapper } = await mountAdminLanguages()
    await wrapper.find('input[type="search"]').setValue('deu')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('[role="status"]').text()).toBe('1 Treffer')
    await wrapper.find('input[type="search"]').setValue('zzz')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state').text()).toContain('Keine Sprache gefunden.')
  })

  it('zeigt „Geändert“ relativ und öffnet das Formular mit dem Namen im Titel; × schließt', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const recent = new Date(Date.now() - 2 * 24 * 3600 * 1000).toISOString()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: makeFetchRouter({ catalogOverride: [{ ID: 1, Code: 'de', Name: 'Deutsch', IsActive: true, UpdatedAt: recent }, { ID: 2, Code: 'en', Name: 'English', IsActive: true }] }) })
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('time').exists()).toBe(true)
    expect(rows[1].find('.cell-changed').text()).toBe('—')
    await menuItem(rows[0], 'Bearbeiten').trigger('click')
    expect(wrapper.find('aside.form-panel h3').text()).toBe('Sprache „Deutsch“ bearbeiten')
    await wrapper.find('.form-panel__close').trigger('click')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  // Regression: leerer Code/Name ging bisher als Serveranfrage raus.
  it('meldet fehlenden Code und Namen am Feld und sendet nichts', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })
    await wrapper.find('button').trigger('click')
    const before = fetchMock.mock.calls.length
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#lang-code').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('#lang-name').attributes('aria-invalid')).toBe('true')
    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('verlangt beim Bearbeiten keinen Code (er ist nicht änderbar)', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })
    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'PUT')).toBe(true)
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const { wrapper } = await mountAdminLanguages()
    expect(await axe(wrapper)).toHaveNoViolations()

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('zeigt Katalog und Formular nebeneinander, Aktiv/Inaktiv als StatusBadge', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const { wrapper } = await mountAdminLanguages()
    expect(wrapper.find('.split').classes()).not.toContain('split--open')
    expect(wrapper.find('.st-badge').exists()).toBe(true)

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('table.data-table').exists()).toBe(true)
    expect(wrapper.find('aside.form-panel').attributes('aria-label')).toContain('bearbeiten')
  })

  it('zeigt bei leerem Katalog den EmptyState', async () => {
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) => (/\/api\/languages$/.test(url) && !options.method)
      ? { ok: true, status: 200, json: async () => [] }
      : base(url, options))
    const { wrapper } = await mountAdminLanguages({ fetchImpl })
    expect(wrapper.find('.comp-card .empty-state__title').text()).toContain('Noch keine Sprachen')
  })

  it('zeigt inaktive Sprachen als neutrales StatusBadge mit Text', async () => {
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) => (/\/api\/languages$/.test(url) && !options.method)
      ? { ok: true, status: 200, json: async () => [{ ID: 9, Code: 'fr', Name: 'Français', IsActive: false }] }
      : base(url, options))
    const { wrapper } = await mountAdminLanguages({ fetchImpl })
    const badge = wrapper.find('tbody .st-badge')
    expect(badge.classes()).toContain('st-badge--unknown')
    expect(badge.text()).toBe(i18n.global.t('languages.statusInactive'))
  })

  // Format des Sprachcodes (BCP47 light) am Feld, Server-400 ebenso.
  it('meldet einen ungültigen Sprachcode am Feld und sendet nichts', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })
    await wrapper.find('button').trigger('click')
    await wrapper.find('#lang-code').setValue('x1')
    await wrapper.find('#lang-name').setValue('Test')
    const before = fetchMock.mock.calls.length
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#lang-code').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('.form-field__message--error').text()).toContain('Ungültiger Sprachcode')
    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('akzeptiert Codes mit Region und Großschreibung', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminLanguages({ fetchImpl: fetchMock })
    await wrapper.find('button').trigger('click')
    await wrapper.find('#lang-code').setValue('PT-br')
    await wrapper.find('#lang-name').setValue('Português')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(true)
  })

  it('zeigt language_code_invalid vom Server am Codefeld', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) => {
      if (options.method === 'POST' && /\/api\/languages$/.test(url)) return jsonResponse({ error: 'x', code: 'language_code_invalid' }, 400)
      return base(url, options)
    })
    const { wrapper } = await mountAdminLanguages({ fetchImpl })
    await wrapper.find('button').trigger('click')
    await wrapper.find('#lang-code').setValue('fr')
    await wrapper.find('#lang-name').setValue('Français')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#lang-code').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('.form-field__message--error').text()).toContain('Ungültiger Sprachcode')
  })
  // Beim Anlegen blieb „Sprachauswahl gespeichert.“ vom vorigen Schritt stehen, eine eigene Meldung fehlte.
  it('meldet das Anlegen einer Sprache mit eigener Meldung statt der alten', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const { wrapper } = await mountAdminLanguages()
    wrapper.vm.showAlert('Sprachauswahl gespeichert.', 'success')
    await flushPromises()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Sprache')).trigger('click')
    await wrapper.find('#lang-code').setValue('fr')
    await wrapper.find('#lang-name').setValue('Français')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Sprachauswahl gespeichert.')
    expect(wrapper.find('.alert-success').text()).toContain('Sprache angelegt.')
  })

  it('meldet das Löschen einer Sprache als Erfolg', async () => {
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    stubConfirm(true)
    const { wrapper } = await mountAdminLanguages()

    await menuItem(wrapper.findAll('tbody tr')[1], 'Löschen').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-success').text()).toContain('Gelöscht.')
  })
})
