import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import AdminRoles from './AdminRoles.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'

function rolesFixture() {
  return [{ ID: 1, Name: 'Viewer', Permissions: 'components:read,incidents:read' }]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ loadOk = true, writeOk = true } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/api\/roles$/.test(url)) {
      return loadOk ? jsonResponse(rolesFixture()) : jsonResponse({ error: 'kaputt' }, 500)
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

// Zeilenaktionen stehen im Zeilenmenü (RowMenu), die Einträge sind auch geschlossen im DOM.
const menuItem = (row, text) => row.findAll('[role="menuitem"]').find(i => i.text().includes(text))

async function mountAdminRoles({ fetchImpl, isGlobalAdmin = true } = {}) {
  useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: isGlobalAdmin })
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminRoles, { global: { plugins: [i18n] } })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminRoles', () => {
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

  it('lädt Rollen und zeigt sie in der Tabelle', async () => {
    const { wrapper } = await mountAdminRoles()

    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('Viewer')
    // kompakte Zusammenfassung statt einer Pille je Recht.
    expect(row.findAll('.tag-pill').map(p => p.text())).toEqual(['Komponenten: R', 'Vorfälle: R'])
  })

  // Die Rechte-Liste läuft auch bei vielen Rechten nicht über: höchstens
  // drei Einträge plus „+n“, die Zelle trägt die vollständige Aufzählung.
  it('kürzt die Rechte-Liste einer Rolle mit vielen Rechten auf wenige Einträge', async () => {
    const all = ['components', 'incidents', 'maintenance', 'notices', 'subscribers', 'sso', 'languages']
      .flatMap(r => ['create', 'read', 'update', 'delete'].map(a => `${r}:${a}`)).join(',')
    const fetchMock = vi.fn(async () => jsonResponse([{ ID: 3, Name: 'Viele', Permissions: all + ',admin_dashboard' }]))
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })

    const pills = wrapper.findAll('tbody tr .tag-pill').map(p => p.text())
    expect(pills).toEqual(['Komponenten: CRUD', 'Vorfälle: CRUD', 'Wartungsfenster: CRUD', '+5'])
    expect(wrapper.find('tbody tr .perm-summary').attributes('title')).toContain('Abonnenten: Anlegen, Lesen, Ändern, Löschen')
    expect(wrapper.find('tbody tr .sr-only').text()).toContain('Admin-Dashboard')
  })

  it('zeigt „Alle Rechte“ für eine Rolle mit allen Rechten', async () => {
    const { RESOURCES } = await import('../../utils/permissions')
    const all = RESOURCES.flatMap(r => ['create', 'read', 'update', 'delete'].map(a => `${r.key}:${a}`)).join(',')
    const { wrapper } = await mountAdminRoles({ fetchImpl: vi.fn(async () => jsonResponse([{ ID: 4, Name: 'Global Admin', Permissions: all }])) })
    expect(wrapper.findAll('tbody tr .tag-pill').map(p => p.text())).toEqual(['Alle Rechte'])
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountAdminRoles({ fetchImpl: makeFetchRouter({ loadOk: false }) })

    expect(wrapper.find('.alert-warning').text()).toContain('kaputt')
  })

  it('zeigt weder "+ Neue Rolle" noch die Aktionen-Spalte für Nicht-Global-Admins', async () => {
    const { wrapper } = await mountAdminRoles({ isGlobalAdmin: false })

    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('.actions-col').exists()).toBe(false)
    expect(wrapper.find('[role="menuitem"]').exists()).toBe(false)
  })

  it('legt eine neue Rolle mit den gewählten Berechtigungen per POST an', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#role-name').setValue('Editor')
    await wrapper.find('input[value="incidents:update"]').setValue(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    // Ändern setzt Lesen mit.
    expect(fetchMock).toHaveBeenCalledWith('/api/roles', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ Name: 'Editor', Permissions: 'incidents:update,incidents:read' }),
    }))
    expect(wrapper.find('.edit-card').exists()).toBe(false)
  })

  it('lädt eine bestehende Rolle zum Bearbeiten inkl. gesplitteter Permissions', async () => {
    const { wrapper } = await mountAdminRoles()

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

    expect(wrapper.find('#role-name').element.value).toBe('Viewer')
    expect(wrapper.find('input[value="components:read"]').element.checked).toBe(true)
    expect(wrapper.find('input[value="incidents:read"]').element.checked).toBe(true)
    expect(wrapper.find('input[value="incidents:update"]').element.checked).toBe(false)
  })

  // Eine Rolle im Altformat (nicht migrierte Datenbank oder Alt-Server) lädt im
  // Editor als CRUD; Speichern schreibt nie mehr ":write".
  it('lädt eine Alt-Rolle mit :write als create/read/update/delete und speichert sie ohne :write', async () => {
    const fetchMock = vi.fn(async (url, options = {}) => (options.method === 'PUT'
      ? jsonResponse({})
      : jsonResponse([{ ID: 5, Name: 'Alt', Permissions: 'notices:write,admin_dashboard' }])))
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })
    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

    for (const a of ['create', 'read', 'update', 'delete']) {
      expect(wrapper.find(`input[value="notices:${a}"]`).element.checked).toBe(true)
    }
    expect(wrapper.find('input[value="admin_dashboard"]').element.checked).toBe(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    const put = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(JSON.parse(put[1].body).Permissions).toBe('notices:create,notices:read,notices:update,notices:delete,admin_dashboard')
    expect(put[1].body).not.toContain(':write')
  })

  // Die Matrix muss in die Formularbreite passen: sie steht in der Formularkarte, ihr
  // Scrollbereich begrenzt sich selbst (siehe PermissionMatrix.test.js für die CSS-Regeln).
  it('zeigt die Matrix innerhalb der Formularkarte mit allen vier Aktionsspalten', async () => {
    const { wrapper } = await mountAdminRoles()
    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    const panel = wrapper.find('aside.form-panel')
    expect(panel.find('.perm-matrix .perm-matrix-scroll').exists()).toBe(true)
    expect(panel.findAll('thead .perm-abbr').map(e => e.text())).toEqual(['C', 'R', 'U', 'D'])
    expect(panel.findAll('thead .perm-action-text').map(e => e.text())).toEqual(['Anlegen', 'Lesen', 'Ändern', 'Löschen'])
  })

  it('löscht eine Rolle nach Bestätigung per DELETE', async () => {
    const confirm = stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith('Rolle „Viewer“ wirklich löschen?')
    expect(fetchMock).toHaveBeenCalledWith('/api/roles/1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('löscht nicht, wenn die Bestätigung abgelehnt wird', async () => {
    stubConfirm(false)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })
    fetchMock.mockClear()

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern fehlschlägt', async () => {
    const { wrapper } = await mountAdminRoles({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#role-name').setValue('Editor')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.alert-warning').text()).toContain('Speichern fehlgeschlagen')
  })

  it('bearbeitet eine Rolle per PUT und bricht das Bearbeiten ab', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
    expect(wrapper.find('.edit-card').exists()).toBe(false)

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await wrapper.find('input[value="incidents:update"]').setValue(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const put = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(put[0]).toBe('/api/roles/1')
    expect(JSON.parse(put[1].body).Permissions).toBe('components:read,incidents:read,incidents:update')
    expect(wrapper.find('.edit-card').exists()).toBe(false)
  })

  it('zeigt die Berechtigungs-Matrix mit allen Bereichen und dem Abschnitt „Zugriff“', async () => {
    const { wrapper } = await mountAdminRoles()
    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

    expect(wrapper.findAll('tbody tr[data-area]').map(r => r.find('.perm-label').text())).toEqual([
      'Übersicht', 'Komponenten', 'Externe Quellen', 'Vorfälle', 'Wartungsfenster', 'Ankündigungen', 'Vorlagen', 'Abonnenten',
      'Mail-Templates', 'Sprachen', 'UI-Texte',
      'Statusseiten', 'Benutzer', 'Rollen & Rechte', 'Tags', 'Audit-Log',
      'Monitoring', 'SSO Konfiguration', 'Systemeinstellungen',
    ])
    expect(wrapper.find('.perm-access .perm-label').text()).toBe('Admin-Dashboard')
    expect(wrapper.find('input[data-column="read"]').element.indeterminate).toBe(true)
  })

  it('speichert nach Zeilen-/Spalten-Auswahl dasselbe Komma-Format wie bisher', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await wrapper.find('tr[data-area="notices"] input[data-row-all]').setValue(true)
    await wrapper.find('input[value="admin_dashboard"]').setValue(true)
    await wrapper.find('input[data-column="read"]').setValue(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const put = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    const saved = JSON.parse(put[1].body).Permissions.split(',')
    // Zeile „Ankündigungen“ vollständig, Spalte „Lesen“ für alle Seiten-Tabs, Sonderrecht,
    // globale Tabs bleiben gesperrt (kein users:read).
    expect(saved).toEqual(expect.arrayContaining(['notices:create', 'notices:read', 'notices:update', 'notices:delete', 'admin_dashboard', 'components:read', 'incidents:read', 'sso:read', 'subscribers:read']))
    expect(saved.some(p => p.startsWith('users:') || p.startsWith('roles:'))).toBe(false)
    expect(saved.some(p => p.endsWith(':write'))).toBe(false)
  })

  it('öffnet eine Rolle ohne Permissions mit leerer Auswahl', async () => {
    const fetchMock = vi.fn(async () => jsonResponse([{ ID: 2, Name: 'Leer', Permissions: '' }]))
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

    expect(wrapper.findAll('input[type="checkbox"]').every(c => !c.element.checked)).toBe(true)
  })

  it('zeigt eine Fehlermeldung, wenn das Löschen fehlschlägt', async () => {
    stubConfirm(true)
    const { wrapper } = await mountAdminRoles({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-warning').text()).toContain('Löschen fehlgeschlagen')
  })

  // Richtung C: Kartenkopf, Untertitel mit Zählung, Suche,
  // Formular als Seitenkarte mit Namen im Titel, Fehler am Feld.
  it('zeigt Kartenkopf und Untertitel mit Zählung', async () => {
    const { wrapper } = await mountAdminRoles()
    expect(wrapper.find('.comp-card__title').text()).toBe('Alle Rollen')
    expect(wrapper.find('.page-head__subtitle').text()).toBe('1 Rolle')
  })

  it('sucht nach dem Rollennamen; ohne Treffer erscheint der Leerzustand', async () => {
    const { wrapper } = await mountAdminRoles()
    await wrapper.find('input[type="search"]').setValue('view')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('[role="status"]').text()).toBe('1 Treffer')
    await wrapper.find('input[type="search"]').setValue('xyz')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state').text()).toContain('Keine Rolle gefunden.')
  })

  it('öffnet das Formular als Seitenkarte mit dem Namen im Titel und schließt es mit ×', async () => {
    const { wrapper } = await mountAdminRoles()
    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('aside.form-panel h3').text()).toBe('Rolle „Viewer“ bearbeiten')
    await wrapper.find('.form-panel__close').trigger('click')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  // Regression: ein leerer Rollenname ging bisher als Serveranfrage raus.
  it('meldet einen leeren Rollennamen am Feld und sendet nichts', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    const before = fetchMock.mock.calls.length
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('#role-name').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('[role="alert"]').text()).toContain('Bitte einen Rollennamen eingeben.')
    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('zeigt „Geändert“ relativ und Platzhalter ohne Zeitstempel', async () => {
    const recent = new Date(Date.now() - 2 * 3600 * 1000).toISOString()
    const fetchMock = vi.fn(async () => jsonResponse([
      { ID: 1, Name: 'A', Permissions: 'a:read', UpdatedAt: recent },
      { ID: 2, Name: 'B', Permissions: '' },
    ]))
    const { wrapper } = await mountAdminRoles({ fetchImpl: fetchMock })
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('time').exists()).toBe(true)
    expect(rows[1].find('.cell-changed').text()).toBe('—')
    expect(rows[1].findAll('.tag-pill')).toHaveLength(0)
  })

  // Stufe 4d: Seitentitel ist eine h2 (die Admin-Topbar trägt die h1),
  // die leere Liste zeigt den Leerzustand statt einer leeren Tabelle.
  it('rendert den Titel als h2 ohne h1 und zeigt bei leerer Liste den Leerzustand', async () => {
    const { wrapper } = await mountAdminRoles({
      fetchImpl: vi.fn(async () => jsonResponse([])),
    })

    expect(wrapper.find('h2.page-head__title').text()).toBe('Rollen & Berechtigungen')
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.find('table').exists()).toBe(false)
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    const { wrapper } = await mountAdminRoles()
    expect(await axe(wrapper)).toHaveNoViolations()

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
