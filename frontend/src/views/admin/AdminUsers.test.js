import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import AdminUsers from './AdminUsers.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'

function usersFixture() {
  return [
    { ID: 1, Email: 'admin@acme.de', IsGlobalAdmin: true, Tags: [] },
    {
      ID: 2, Email: 'editor@acme.de', IsGlobalAdmin: false, Tags: [{ ID: 5, Name: 'EU' }],
      UserStatusPageRoles: [{ StatusPageID: 10, RoleID: 3 }],
    },
    { ID: 3, Email: 'noaccess@acme.de', IsGlobalAdmin: false, Tags: [], UserStatusPageRoles: [] },
  ]
}

function rolesFixture() {
  // GlobalOnly kommt aus GET /api/roles ("Global Admin").
  return [{ ID: 1, Name: 'Global Admin', GlobalOnly: true }, { ID: 3, Name: 'Editor', GlobalOnly: false }]
}

function pagesFixture() {
  return [{ ID: 10, Name: 'Acme Corp', DefaultRoleID: 0 }]
}

function tagsFixture() {
  // zwei gleichnamige seitengebundene Tags (Acme/TechStart) neben
  // dem globalen; Benutzer-Tags sind nur global zulässig.
  return [
    { ID: 6, Name: 'DE', StatusPageID: 10 },
    { ID: 5, Name: 'EU', StatusPageID: null },
    { ID: 7, Name: 'DE', StatusPageID: 11 },
  ]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ usersOk = true, tagsOk = true, writeOk = true, users = usersFixture(), writeError = { error: 'Speichern fehlgeschlagen' }, writeStatus = 500 } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/api\/users$/.test(url)) {
      return usersOk ? jsonResponse(users) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'GET' && /\/api\/roles$/.test(url)) return jsonResponse(rolesFixture())
    if (method === 'GET' && /\/api\/pages$/.test(url)) return jsonResponse(pagesFixture())
    if (method === 'GET' && /\/api\/tags$/.test(url)) {
      return tagsOk ? jsonResponse(tagsFixture()) : jsonResponse({ error: 'tags kaputt' }, 500)
    }
    if (['POST', 'PUT'].includes(method)) {
      return writeOk ? jsonResponse({}) : jsonResponse(writeError, writeStatus)
    }
    if (method === 'DELETE') {
      return writeOk ? jsonResponse({}) : jsonResponse(writeError, writeStatus)
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let activeWrapper = null

// Zeilenaktionen stehen im Zeilenmenü (RowMenu), die Einträge sind auch geschlossen im DOM.
const menuItem = (row, text) => row.findAll('[role="menuitem"]').find(i => i.text().includes(text))
const editRow = (wrapper, index) => menuItem(wrapper.findAll('tbody tr')[index], 'Bearbeiten').trigger('click')

async function mountUsers({ fetchImpl, isGlobalAdmin = true, currentUser = {} } = {}) {
  useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: isGlobalAdmin, ...currentUser })
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminUsers, { global: { plugins: [i18n] } })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminUsers', () => {
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

  it('lädt Benutzer und fasst deren Zugriff zusammen (Global Admin / Seitenanzahl)', async () => {
    const { wrapper } = await mountUsers()

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('admin@acme.de')
    expect(rows[0].text()).toContain('Global Admin')
    expect(rows[1].text()).toContain('editor@acme.de')
    expect(rows[1].text()).toContain('1 Statusseite')
    expect(rows[1].text()).not.toContain('Statusseite(n)')
    expect(rows[1].text()).toContain('EU')
  })

  // Singular/Plural statt "Statusseite(n)".
  it('wählt die Pluralform der Statusseiten-Zählung (de und en)', async () => {
    const users = [{ ID: 7, Email: 'multi@acme.de', IsGlobalAdmin: false, Tags: [], UserStatusPageRoles: [{ StatusPageID: 10, RoleID: 3 }, { StatusPageID: 11, RoleID: 3 }] }]
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ users }) })
    expect(wrapper.find('tbody tr').text()).toContain('2 Statusseiten')
    expect(i18n.global.t('users.pagesCountSummary', { count: 1 }, { locale: 'en', plural: 1 })).toBe('1 status page')
    expect(i18n.global.t('users.pagesCountSummary', { count: 2 }, { locale: 'en', plural: 2 })).toBe('2 status pages')
  })

  it('nennt in der Löschfrage die E-Mail des Benutzers', async () => {
    const confirm = stubConfirm(true)
    const { wrapper } = await mountUsers({ currentUser: { id: 99 } })
    await menuItem(wrapper.findAll('tbody tr')[1], 'Löschen').trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledWith('Benutzer „editor@acme.de“ wirklich löschen?')
  })

  it('zeigt "Keine Rechte" für Benutzer ohne Global-Admin und ohne Seitenrollen', async () => {
    const { wrapper } = await mountUsers()

    const rows = wrapper.findAll('tbody tr')
    expect(rows[2].text()).toContain('noaccess@acme.de')
    expect(rows[2].text()).toContain('Keine Rechte')
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ usersOk: false }) })

    expect(wrapper.find('.alert-warning').text()).toContain('kaputt')
  })

  it('lädt trotz fehlschlagendem /api/tags weiter (fehlertolerant)', async () => {
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ tagsOk: false }) })

    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
  })

  it('versteckt Neu-Button und Aktionen-Spalte für Nicht-Global-Admins', async () => {
    const { wrapper } = await mountUsers({ isGlobalAdmin: false })

    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('.actions-col').exists()).toBe(false)
    expect(wrapper.find('[role="menuitem"]').exists()).toBe(false)
  })

  it('legt einen neuen Benutzer mit Seitenrolle und Tags per POST an', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#user-email').setValue('new@acme.de')
    await wrapper.find('#user-password').setValue('secret123')
    await wrapper.find('.page-select select').setValue('3')
    // TagCheckboxList (components/TagCheckboxList.vue) rendert keine
    // [value]-Attribute auf ihren Checkboxen - die 2. Checkbox im Formular
    // (nach der Global-Admin-Checkbox) ist die des einzigen Fixture-Tags "EU".
    await wrapper.findAll('input[type="checkbox"]')[1].setValue(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const [url, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST')
    expect(url).toBe('/api/users')
    const payload = JSON.parse(options.body)
    expect(payload).toMatchObject({
      email: 'new@acme.de', password: 'secret123', tag_ids: [5], is_global_admin: false,
      page_roles: [{ status_page_id: 10, role_id: 3 }],
    })
  })

  // Die Rolle einer Seite lässt sich wieder auf "keine" stellen; die Seite wird
  // dann nicht mehr mitgeschickt (das Backend entfernt fehlende Seitenrollen).
  it('sendet keine Seitenrolle mehr, wenn die Auswahl geleert wird', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })

    await editRow(wrapper, 1)
    await wrapper.find('.page-select select').setValue('')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(JSON.parse(options.body).page_roles).toEqual([])
  })

  // Fehlende Listen im Antwortkörper (null) und Benutzer ohne Tags/Admin-Flag
  // lassen die Oberfläche nicht abstürzen.
  it('kommt mit leeren Antworten und Benutzern ohne Tags zurecht', async () => {
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'GET' && /\/api\/users$/.test(url)) return jsonResponse([{ ID: 9, Email: 'schlank@acme.de' }])
      if (method === 'GET' && /\/api\/(roles|pages|tags)$/.test(url)) return jsonResponse(null)
      throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
    })
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)

    await editRow(wrapper, 0)
    expect(wrapper.find('#user-email').element.value).toBe('schlank@acme.de')
    expect(wrapper.find('input[type="checkbox"]').element.checked).toBe(false)
  })

  it('lädt einen bestehenden Benutzer zum Bearbeiten inkl. Seitenrollen', async () => {
    const { wrapper } = await mountUsers()

    await editRow(wrapper, 1)

    expect(wrapper.find('#user-email').element.value).toBe('editor@acme.de')
    expect(wrapper.find('.page-select select').element.value).toBe('3')
  })

  it('blendet den Seitenzugriff-Bereich aus, wenn "Global Admin" aktiviert ist', async () => {
    const { wrapper } = await mountUsers()

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    expect(wrapper.find('.page-roles-list').exists()).toBe(true)

    // Erste Checkbox im Formular ist "Global Admin" (vor der TagCheckboxList).
    await wrapper.findAll('input[type="checkbox"]')[0].setValue(true)
    expect(wrapper.find('.page-roles-list').exists()).toBe(false)
  })

  // Die Login-Antwort lieferte keine id, der Vergleich mit
  // currentUser.id griff nie und die eigene Zeile hatte einen Löschen-Knopf.
  it('bietet für die eigene Zeile kein Löschen an, sondern verweist aufs Profil', async () => {
    const users = [...usersFixture(), { ID: 4, Email: 'me@acme.de', IsGlobalAdmin: true, Tags: [] }]
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ users }), currentUser: { id: 4 } })

    const meRow = wrapper.findAll('tbody tr')[3]
    expect(menuItem(meRow, 'Löschen')).toBeUndefined()
    expect(meRow.text()).toContain('Eigenes Konto')
    // Zwei Global Admins: der andere bleibt löschbar.
    expect(menuItem(wrapper.findAll('tbody tr')[0], 'Löschen')).toBeDefined()
  })

  it('bietet für den letzten Global Admin kein Löschen an', async () => {
    const { wrapper } = await mountUsers({ currentUser: { id: 99 } })

    const adminRow = wrapper.findAll('tbody tr')[0]
    expect(menuItem(adminRow, 'Löschen')).toBeUndefined()
    expect(adminRow.text()).toContain('Letzter Global Admin')
  })

  it('sperrt beim eigenen Konto den Global-Admin-Schalter mit Erklärung', async () => {
    const users = [...usersFixture(), { ID: 4, Email: 'me@acme.de', IsGlobalAdmin: true, Tags: [] }]
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ users }), currentUser: { id: 4 } })

    await editRow(wrapper, 3)
    const adminBox = wrapper.findAll('input[type="checkbox"]')[0]
    expect(adminBox.attributes('disabled')).toBeDefined()
    expect(wrapper.find('#user-admin-lock-hint').text()).toContain('nicht selbst entziehen')

    await wrapper.findAll('button').find(b => b.text().includes('Abbrechen')).trigger('click')
    await editRow(wrapper, 0)
    expect(wrapper.findAll('input[type="checkbox"]')[0].attributes('disabled')).toBeUndefined()
  })

  it('sperrt beim letzten Global Admin den Global-Admin-Schalter', async () => {
    const { wrapper } = await mountUsers({ currentUser: { id: 99 } })

    await editRow(wrapper, 0)
    expect(wrapper.findAll('input[type="checkbox"]')[0].attributes('disabled')).toBeDefined()
    expect(wrapper.find('#user-admin-lock-hint').text()).toContain('letzte Global Admin')
  })

  it('übersetzt den Fehlercode des Backends (409 last_global_admin)', async () => {
    stubConfirm(true)
    const users = [...usersFixture(), { ID: 4, Email: 'other@acme.de', IsGlobalAdmin: true, Tags: [] }]
    const fetchImpl = makeFetchRouter({ users, writeOk: false, writeStatus: 409, writeError: { error: 'x', code: 'last_global_admin' } })
    const { wrapper } = await mountUsers({ fetchImpl, currentUser: { id: 99 } })

    await menuItem(wrapper.findAll('tbody tr')[3], 'Löschen').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-warning').text()).toContain('Ernenne zuerst einen weiteren Global Admin')
  })

  it('löscht einen Benutzer nach Bestätigung per DELETE', async () => {
    stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock, currentUser: { id: 99 } })

    await menuItem(wrapper.findAll('tbody tr')[1], 'Löschen').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/users/2', expect.objectContaining({ method: 'DELETE' }))
  })

  // "Global Admin" ist eine globale Eigenschaft, keine Seitenrolle.
  it('bietet "Global Admin" nicht als Seitenrolle an', async () => {
    const { wrapper } = await mountUsers()

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    const options = wrapper.find('.page-select select').findAll('option').map(o => o.text())
    expect(options).toContain('Editor')
    expect(options).not.toContain('Global Admin')
  })

  it('zeigt eine schon vergebene "Global Admin"-Seitenrolle (Altbestand) weiter an', async () => {
    const users = [{ ID: 8, Email: 'legacy@acme.de', IsGlobalAdmin: false, Tags: [], UserStatusPageRoles: [{ StatusPageID: 10, RoleID: 1 }] }]
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ users }) })

    await editRow(wrapper, 0)
    expect(wrapper.find('.page-select select').element.value).toBe('1')
  })

  // seitengebundene Tags (gleichnamig je Seite) erschienen doppelt,
  // obwohl das Backend sie für Benutzer mit 400 ablehnt.
  it('bietet nur globale Tags an, keine seitengebundenen Duplikate', async () => {
    const { wrapper } = await mountUsers()

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    const tagLabels = wrapper.findAll('.tag-checkbox').map(l => l.text())
    expect(tagLabels).toEqual(['EU'])
  })

  // Richtung C: Kartenkopf, Untertitel mit Zählung, Suche,
  // Formular als Seitenkarte mit Namen im Titel, Fehler am Feld.
  it('zeigt Kartenkopf und Untertitel mit Zählung', async () => {
    const { wrapper } = await mountUsers()
    expect(wrapper.find('.comp-card__title').text()).toBe('Alle Benutzer')
    expect(wrapper.find('.page-head__subtitle').text()).toBe('3 Benutzer · 1 Global Admin')
  })

  it('sucht nach E-Mail und Tag; ohne Treffer erscheint der Leerzustand', async () => {
    const { wrapper } = await mountUsers()
    await wrapper.find('input[type="search"]').setValue('editor')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('[role="status"]').text()).toBe('1 Treffer')
    await wrapper.find('input[type="search"]').setValue('eu')
    expect(wrapper.findAll('tbody tr')[0].text()).toContain('editor@acme.de')
    await wrapper.find('input[type="search"]').setValue('niemand')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state').text()).toContain('Kein Benutzer gefunden.')
  })

  it('öffnet das Formular als Seitenkarte mit der E-Mail im Titel und schließt es mit ×', async () => {
    const { wrapper } = await mountUsers()
    await editRow(wrapper, 1)
    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('aside.form-panel h3').text()).toBe('Benutzer „editor@acme.de“ bearbeiten')
    await wrapper.find('.form-panel__close').trigger('click')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  // Regression: fehlende Pflichtfelder gingen bisher als Serveranfrage raus.
  it('meldet fehlende E-Mail und fehlendes Passwort (neu) am Feld und sendet nichts', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    const before = fetchMock.mock.calls.length
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#user-email').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('#user-password').attributes('aria-invalid')).toBe('true')
    const alerts = wrapper.findAll('aside [role="alert"]').map(a => a.text())
    expect(alerts.some(a => a.includes('E-Mail-Adresse'))).toBe(true)
    expect(alerts.some(a => a.includes('Passwort'))).toBe(true)
    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('verlangt beim Bearbeiten kein neues Passwort', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    await editRow(wrapper, 1)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'PUT')).toBe(true)
  })

  it('zeigt Tags als Pillen, den Zugriff mit Hervorhebung für Global Admins und „Geändert“ relativ', async () => {
    const recent = new Date(Date.now() - 5 * 24 * 3600 * 1000).toISOString()
    const users = [{ ...usersFixture()[0], UpdatedAt: recent }, usersFixture()[1]]
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ users }) })
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('.tag-pill--accent').text()).toBe('Global Admin')
    expect(rows[0].find('time').exists()).toBe(true)
    expect(rows[1].find('.tag-pill--accent').exists()).toBe(false)
    expect(rows[1].findAll('.tag-pills .tag-pill').map(p => p.text())).toEqual(['EU'])
    expect(rows[1].find('.cell-changed').text()).toBe('—')
  })

  it('kommt mit Benutzern ohne Tags-Feld zurecht (Anzeige und Suche)', async () => {
    const users = [{ ID: 7, Email: 'ohne@acme.de', IsGlobalAdmin: false }]
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ users }) })
    expect(wrapper.find('tbody tr td:nth-child(3)').text()).toBe('—')
    await wrapper.find('input[type="search"]').setValue('ohne')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })

  // Stufe 4d: h2 statt h1 (die Admin-Topbar trägt die h1), Leerzustand,
  // Formularfelder über FormField (Pflicht-Stern, Passwort-Hinweis nur beim Bearbeiten).
  it('rendert den Titel als h2 ohne h1 und zeigt bei leerer Liste den Leerzustand', async () => {
    const { wrapper } = await mountUsers({ fetchImpl: makeFetchRouter({ users: [] }) })

    expect(wrapper.find('h2.page-head__title').exists()).toBe(true)
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('.empty-state').text()).toContain('Keine Benutzer gefunden')
    expect(wrapper.find('table').exists()).toBe(false)
  })

  it('zeigt im Formular Pflichtmarker und den Passwort-Hinweis nur beim Bearbeiten', async () => {
    const { wrapper } = await mountUsers()

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    expect(wrapper.find('#user-email').attributes('required')).toBeDefined()
    expect(wrapper.find('#user-password').attributes('required')).toBeDefined()
    expect(wrapper.find('#user-password').attributes('aria-describedby')).toBeUndefined()

    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
    await editRow(wrapper, 1)
    expect(wrapper.find('#user-password').attributes('required')).toBeUndefined()
    const hintId = wrapper.find('#user-password').attributes('aria-describedby')
    expect(wrapper.find(`#${hintId}`).text()).toContain('leer lassen')
  })

  it('zeigt im Formular den Leerzustand, wenn es keine Statusseiten gibt', async () => {
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options) =>
      /\/api\/pages$/.test(url) ? jsonResponse([]) : base(url, options))
    const { wrapper } = await mountUsers({ fetchImpl })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    expect(wrapper.find('.page-roles-list .empty-state').text()).toContain('Lege zuerst eine Statusseite an')
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    const { wrapper } = await mountUsers()
    expect(await axe(wrapper)).toHaveNoViolations()

    await editRow(wrapper, 0)
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Passwort-Mindestlänge steht am Feld, bevor etwas gesendet wird,
  // und ein Backend-400 password_too_short landet ebenfalls am Feld.
  it('meldet ein zu kurzes Passwort am Feld und sendet nichts', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#user-email').setValue('new@acme.de')
    await wrapper.find('#user-password').setValue('kurz')
    const before = fetchMock.mock.calls.length
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#user-password').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('aside [role="alert"]').text()).toContain('mindestens 8 Zeichen')
    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('meldet ein zu langes Passwort (mehr als 72 Byte) am Feld und sendet nichts', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#user-email').setValue('new@acme.de')
    // 40 Umlaute sind 80 Byte, aber nur 40 Zeichen.
    await wrapper.find('#user-password').setValue('ä'.repeat(40))
    const before = fetchMock.mock.calls.length
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#user-password').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('aside [role="alert"]').text()).toContain('höchstens 72 Byte')
    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('zeigt password_too_short vom Server am Passwortfeld statt als Sammelmeldung', async () => {
    const fetchMock = makeFetchRouter({ writeOk: false, writeStatus: 400, writeError: { error: 'x', code: 'password_too_short' } })
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#user-email').setValue('new@acme.de')
    await wrapper.find('#user-password').setValue('genug-lang-123')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#user-password').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('aside [role="alert"]').text()).toContain('mindestens 8 Zeichen')
  })

  it('prüft beim Ändern nur ein gesetztes Passwort; leer bleibt erlaubt', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    await editRow(wrapper, 1)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'PUT')).toBe(true)
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  it('zeigt andere Serverfehler beim Speichern als Sammelmeldung, nicht am Passwortfeld', async () => {
    const fetchMock = makeFetchRouter({ writeOk: false, writeStatus: 500, writeError: { error: 'Datenbank kaputt' } })
    const { wrapper } = await mountUsers({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#user-email').setValue('new@acme.de')
    await wrapper.find('#user-password').setValue('genug-lang-123')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.text()).toContain('Datenbank kaputt')
    expect(wrapper.find('#user-password').attributes('aria-invalid')).toBeUndefined()
  })
})
