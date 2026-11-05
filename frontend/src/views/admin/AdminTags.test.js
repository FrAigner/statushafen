import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import AdminTags from './AdminTags.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'

function tagsFixture() {
  return [
    { ID: 1, Name: 'EU', StatusPageID: null },
    { ID: 2, Name: 'Beta-Feature', StatusPageID: 5 },
  ]
}

function pagesFixture() {
  return [{ ID: 5, Name: 'Acme Corp' }]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ tagsOk = true, pagesOk = true, writeOk = true } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/api\/tags$/.test(url)) {
      return tagsOk ? jsonResponse(tagsFixture()) : jsonResponse({ error: 'Tags kaputt' }, 500)
    }
    if (method === 'GET' && /\/api\/pages$/.test(url)) {
      return pagesOk ? jsonResponse(pagesFixture()) : jsonResponse({ error: 'Pages kaputt' }, 500)
    }
    if (['POST', 'PUT'].includes(method)) {
      return writeOk ? jsonResponse({}) : jsonResponse({ error: 'Speichern fehlgeschlagen' }, 500)
    }
    if (method === 'DELETE') {
      return writeOk ? jsonResponse({}) : jsonResponse({ error: 'Löschen fehlgeschlagen' }, 500)
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let activeWrapper = null

// Zeilenaktionen stehen im Zeilenmenü (RowMenu); die Einträge sind
// auch bei geschlossenem Menü im DOM.
const menuItem = (row, text) => row.findAll('[role="menuitem"]').find(i => i.text().includes(text))

async function mountAdminTags({ fetchImpl, isGlobalAdmin = true } = {}) {
  useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: isGlobalAdmin })
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminTags, { global: { plugins: [i18n] } })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminTags', () => {
  beforeEach(() => {
    useAuth().logout()
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('lädt Tags + Seiten und zeigt globale sowie seitengebundene Tags', async () => {
    const { wrapper } = await mountAdminTags()

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('EU')
    expect(rows[1].text()).toContain('Beta-Feature')
    expect(rows[1].text()).toContain('Acme Corp')
  })

  it('filtert die Tabelle nach Scope (global / bestimmte Seite)', async () => {
    const { wrapper } = await mountAdminTags()

    await wrapper.find('select.comp-filter').setValue('global')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody tr').text()).toContain('EU')

    await wrapper.find('select.comp-filter').setValue('5')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody tr').text()).toContain('Beta-Feature')
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountAdminTags({ fetchImpl: makeFetchRouter({ tagsOk: false }) })

    expect(wrapper.find('.alert-warning').text()).toContain('Tags kaputt')
  })

  it('legt einen neuen globalen Tag per POST an', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminTags({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#tag-name').setValue('Neuer Tag')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/tags', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ name: 'Neuer Tag' }),
    }))
  })

  it('legt einen seitengebundenen Tag mit status_page_id an', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminTags({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#tag-name').setValue('Seiten-Tag')
    await wrapper.find('#tag-scope').setValue('5')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/tags', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ name: 'Seiten-Tag', status_page_id: 5 }),
    }))
  })

  it('benennt einen bestehenden Tag per PUT um, ohne den Scope zu ändern', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminTags({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Umbenennen').trigger('click')
    expect(wrapper.find('#tag-name').element.value).toBe('EU')
    expect(wrapper.find('#tag-scope').exists()).toBe(false)

    await wrapper.find('#tag-name').setValue('EU-Renamed')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/tags/1', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ name: 'EU-Renamed' }),
    }))
  })

  it('löscht einen Tag nach Bestätigung mit Namen im confirm-Text', async () => {
    const confirm = stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminTags({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('EU'))
    expect(fetchMock).toHaveBeenCalledWith('/api/tags/1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('versteckt Neu-Button und Aktionen-Spalte für Nicht-Global-Admins', async () => {
    const { wrapper } = await mountAdminTags({ isGlobalAdmin: false })

    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('.actions-col').exists()).toBe(false)
    expect(wrapper.find('[role="menuitem"]').exists()).toBe(false)
  })

  // Stufe 4d: h2 statt h1 (die Topbar trägt die h1), Leerzustand
  // statt leerer Tabelle, Feldhinweis per aria-describedby über FormField.
  it('rendert den Titel als h2 ohne h1 und die Tabelle, solange Tags vorhanden sind', async () => {
    const { wrapper } = await mountAdminTags()
    expect(wrapper.find('h2.page-head__title').exists()).toBe(true)
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(false)

    expect(wrapper.find('table').exists()).toBe(true)
  })

  it('zeigt bei leerer Tag-Liste den Leerzustand statt der Tabelle', async () => {
    const router = vi.fn(async (url) => jsonResponse(/\/api\/tags$/.test(url) ? [] : pagesFixture()))
    const { wrapper } = await mountAdminTags({ fetchImpl: router })
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.find('table').exists()).toBe(false)
  })

  // Richtung C: Karte mit Kopf, Suche, Untertitel mit Zählung,
  // Formular als Seitenkarte mit Namen im Titel und Fehler am Feld.
  it('zeigt Kartenkopf und Untertitel mit Zählung', async () => {
    const { wrapper } = await mountAdminTags()
    expect(wrapper.find('.comp-card__title').text()).toBe('Alle Tags')
    expect(wrapper.find('.page-head__subtitle').text()).toBe('2 Tags · davon 1 global')
  })

  it('sucht nach dem Namen und meldet die Trefferzahl; ohne Treffer erscheint der Leerzustand', async () => {
    const { wrapper } = await mountAdminTags()
    await wrapper.find('input[type="search"]').setValue('beta')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('[role="status"]').text()).toBe('1 Treffer')

    await wrapper.find('input[type="search"]').setValue('gibtesnicht')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state').text()).toContain('Kein Tag gefunden.')
  })

  it('zeigt „Geändert“ relativ und nutzt für den Scope Symbol und Text', async () => {
    const recent = new Date(Date.now() - 3 * 24 * 3600 * 1000).toISOString()
    const router = vi.fn(async (url) => jsonResponse(/\/api\/tags$/.test(url) ? [{ ID: 1, Name: 'EU', StatusPageID: null, UpdatedAt: recent }, { ID: 2, Name: 'Alt', StatusPageID: 5 }] : pagesFixture()))
    const { wrapper } = await mountAdminTags({ fetchImpl: router })
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('time').text()).toMatch(/3/)
    expect(rows[0].find('.status-badge svg').exists()).toBe(true)
    expect(rows[1].find('time').exists()).toBe(false)
    expect(rows[1].find('.cell-changed').text()).toBe('—')
  })

  it('öffnet das Formular als Seitenkarte mit dem Namen im Titel und schließt es mit ×', async () => {
    const { wrapper } = await mountAdminTags()
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
    await menuItem(wrapper.find('tbody tr'), 'Umbenennen').trigger('click')
    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('aside.form-panel h3').text()).toBe('Tag „EU“ umbenennen')
    expect(wrapper.find('aside.form-panel').text()).toContain('Felder mit * sind Pflichtfelder.')
    await wrapper.find('.form-panel__close').trigger('click')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  // Regression: Ein leerer Name ging bisher als Serveranfrage raus (Fehlermeldung
  // vom Backend statt am Feld). Jetzt Fehler am Feld, Fokus aufs Feld, kein Request.
  it('meldet einen leeren Namen am Feld, fokussiert es und sendet nichts', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminTags({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    const callsBefore = fetchMock.mock.calls.length
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#tag-name').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('[role="alert"]').text()).toContain('Bitte einen Namen eingeben.')
    expect(fetchMock.mock.calls.length).toBe(callsBefore)
  })

  it('zeigt beim Umbenennen eines seitengebundenen Tags den Seitennamen statt „Global“', async () => {
    const { wrapper } = await mountAdminTags()
    await menuItem(wrapper.findAll('tbody tr')[1], 'Umbenennen').trigger('click')
    const scope = wrapper.find('aside.form-panel .adm-subhead + div')
    expect(scope.find('.tag-pill').text()).toBe('Acme Corp')
    expect(scope.find('.status-badge').exists()).toBe(false)
  })

  it('löscht beim Löschen nichts ohne Bestätigung', async () => {
    stubConfirm(false)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminTags({ fetchImpl: fetchMock })
    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    expect(fetchMock.mock.calls.some(c => c[1]?.method === 'DELETE')).toBe(false)
  })

  it('verbindet im Anlegen-Formular den Bereichshinweis per aria-describedby', async () => {
    const { wrapper } = await mountAdminTags()
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')

    expect(wrapper.find('#tag-name').attributes('required')).toBeDefined()
    const hintId = wrapper.find('#tag-scope').attributes('aria-describedby')
    expect(wrapper.find(`#${hintId}`).exists()).toBe(true)
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    const { wrapper } = await mountAdminTags()
    expect(await axe(wrapper)).toHaveNoViolations()

    await menuItem(wrapper.find('tbody tr'), 'Umbenennen').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
  // Die rote Fehlermeldung eines Duplikats blieb nach dem erfolgreichen Speichern stehen.
  it('entfernt die Fehlermeldung beim nächsten Speichern und meldet den Erfolg', async () => {
    let failWrites = true
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) =>
      failWrites && options.method === 'POST' ? jsonResponse({ error: 'existiert bereits' }, 409) : base(url, options))
    const { wrapper } = await mountAdminTags({ fetchImpl })
    const save = async (name) => {
      await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
      await wrapper.find('#tag-name').setValue(name)
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
    }

    await save('EU')
    expect(wrapper.find('.alert-warning').text()).toContain('existiert bereits')

    failWrites = false
    await save('Neuer Tag')

    expect(wrapper.text()).not.toContain('existiert bereits')
    expect(wrapper.find('.alert-success').text()).toContain('Tag angelegt.')
  })

  it('meldet das Löschen eines Tags als Erfolg', async () => {
    stubConfirm(true)
    const { wrapper } = await mountAdminTags()

    await menuItem(wrapper.findAll('tbody tr')[0], 'Löschen').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-success').text()).toContain('Tag gelöscht.')
    await wrapper.find('.alert-close').trigger('click')
    expect(wrapper.find('.alert').exists()).toBe(false)
  })
  it('zeigt einen Fehler als Warnung, wenn das Löschen fehlschlägt', async () => {
    stubConfirm(true)
    const { wrapper } = await mountAdminTags({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await menuItem(wrapper.findAll('tbody tr')[0], 'Löschen').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-warning').text()).toContain('Löschen fehlgeschlagen')
  })
})
