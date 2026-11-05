import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { withArchivedComponent, componentTreeFixture, expectedTreeOptionLabels, multiSelectOptionLabels } from '../../test-support/componentTree'
import AdminMaintenance from './AdminMaintenance.vue'
import { axe } from '../../test-support/axe.js'
import { fieldByLabel } from '../../test-support/fieldByLabel.js'
import { clickMenuItem } from '../../test-support/rowMenu.js'
import { expectNoInlineStyles } from '../../test-support/noInlineStyles.js'
import { stubConfirm } from '../../test-support/confirm'

function maintenanceFixture() {
  return [
    {
      ID: 1, Title: 'Datenbank-Upgrade', Message: 'Kurze Unterbrechung.', Status: 'scheduled', IsMaintenance: true,
      Components: [{ ID: 10, Name: 'API' }], Tags: [],
      ScheduledStartAt: '2026-08-01T10:00:00Z', ScheduledEndAt: '2026-08-01T12:00:00Z',
      ReminderLeadMinutes: 120,
    },
  ]
}

function templatesFixture() {
  return [{
    ID: 1, Name: 'DB-Wartung', Kind: 'maintenance', Title: 'Datenbank-Wartung',
    Message: 'Kurze Unterbrechung.', ComponentIDs: '10,11',
  }]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ incidentsOk = true, items = maintenanceFixture(), templates = [], writeOk = true } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/incident-templates$/.test(url)) return jsonResponse(templates)
    if (method === 'GET' && /\/incidents\?is_maintenance=true&limit=200(&offset=\d+)?$/.test(url)) {
      return incidentsOk ? jsonResponse(items) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'POST' && /\/incidents$/.test(url)) {
      return writeOk ? jsonResponse({ ID: 99 }) : jsonResponse({ error: 'Speichern kaputt' }, 500)
    }
    if (method === 'PUT' && /\/incidents\/\d+$/.test(url)) return jsonResponse({ ID: 1 })
    // Baum mit Unterkomponente statt nur oberster Ebene.
    if (method === 'GET' && /\/components$/.test(url)) return jsonResponse(componentTreeFixture())
    if (method === 'GET' && /\/api\/tags/.test(url)) return jsonResponse([])
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let activeWrapper = null

async function mountAdminMaintenance({ fetchImpl, tenantId = '1' } = {}) {
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminMaintenance, {
    global: { plugins: [i18n], stubs: onyxFormStubs },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

// Rechte je Tab statt incidents:write.
const MAINTENANCE_PERMS = 'maintenance:create,maintenance:update,maintenance:delete,incident_templates:read'

describe('AdminMaintenance', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
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

  it('lädt weitere Wartungsfenster nach, wenn die Seite voll war', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const wartung = (ID) => ({ ID, Title: `Wartung ${ID}`, Message: 'm', Status: 'resolved', IsMaintenance: true, Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' })
    const base = makeFetchRouter()
    const fetchMock = vi.fn(async (url, options) => {
      if (url === '/api/pages/1/incidents?is_maintenance=true&limit=200') return jsonResponse(Array.from({ length: 200 }, (_, i) => wartung(i + 1)))
      if (url === '/api/pages/1/incidents?is_maintenance=true&limit=200&offset=200') return jsonResponse([wartung(201)])
      return base(url, options)
    })
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })
    expect(wrapper.findAll('tbody tr')).toHaveLength(200)

    await wrapper.find('[data-test="load-more"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(201)
    expect(wrapper.find('[data-test="load-more"]').exists()).toBe(false)
  })

  it('behält den Knopf bei voller Folgeseite und sperrt ihn während des Ladens', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const wartung = (ID) => ({ ID, Title: `Wartung ${ID}`, Message: 'm', Status: 'resolved', IsMaintenance: true, Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' })
    let release
    const pending = new Promise(resolve => { release = resolve })
    const base = makeFetchRouter()
    const fetchMock = vi.fn(async (url, options) => {
      if (url === '/api/pages/1/incidents?is_maintenance=true&limit=200') return jsonResponse(Array.from({ length: 200 }, (_, i) => wartung(i + 1)))
      if (url === '/api/pages/1/incidents?is_maintenance=true&limit=200&offset=200') {
        await pending
        return jsonResponse(Array.from({ length: 200 }, (_, i) => wartung(i + 201)))
      }
      return base(url, options)
    })
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })

    await wrapper.find('[data-test="load-more"]').trigger('click')
    expect(wrapper.find('[data-test="load-more"]').attributes('disabled')).toBeDefined()
    release()
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(400)
    expect(wrapper.find('[data-test="load-more"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="load-more"]').attributes('disabled')).toBeUndefined()
  })

  it('meldet einen Fehler beim Nachladen weiterer Wartungsfenster und behält die Liste', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const wartung = (ID) => ({ ID, Title: `Wartung ${ID}`, Message: 'm', Status: 'resolved', IsMaintenance: true, Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' })
    const base = makeFetchRouter()
    const fetchMock = vi.fn(async (url, options) => {
      if (url.endsWith('limit=200')) return jsonResponse(Array.from({ length: 200 }, (_, i) => wartung(i + 1)))
      if (url.endsWith('offset=200')) return jsonResponse({ error: 'kaputt' }, 500)
      return base(url, options)
    })
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })

    await wrapper.find('[data-test="load-more"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(200)
    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  // vorher bot die Auswahl nur die oberste Ebene an.
  it('bietet in der Komponenten-Auswahl auch Unterkomponenten eingerückt an', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance()
    await wrapper.find('.page-head button').trigger('click')

    expect(multiSelectOptionLabels(wrapper)).toEqual(expectedTreeOptionLabels)
  })

  // eine inzwischen archivierte Komponente einer bestehenden Wartung bleibt in der Auswahl sichtbar.
  it('zeigt beim Bearbeiten eine archivierte, gewählte Komponente weiter in der Auswahl', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const items = [{ ...maintenanceFixture()[0], Components: [{ ID: 13, Name: 'Altsystem' }] }]
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: withArchivedComponent(makeFetchRouter({ items })) })
    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(multiSelectOptionLabels(wrapper)).toContain('Altsystem')
    await wrapper.find('.page-head button').trigger('click')
    expect(multiSelectOptionLabels(wrapper)).not.toContain('Altsystem')
  })

  // Nachrichtenfeld ist der Editor.
  it('Nachrichtenfeld ist der RichTextEditor', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance()
    await wrapper.find('.page-head button').trigger('click')
    expect(fieldByLabel(wrapper, 'Nachricht / Details').element.closest('.rte')).not.toBeNull()
  })

  it('lädt Wartungsfenster und formatiert Start-/Endzeit im deutschen Format', async () => {
    const { wrapper } = await mountAdminMaintenance()

    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('Datenbank-Upgrade')
    const expectedStart = new Date('2026-08-01T10:00:00Z').toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' })
    expect(row.text()).toContain(expectedStart)
  })

  it('formatiert Start-/Endzeit im englischen Format, wenn die UI-Sprache Englisch ist', async () => {
    i18n.global.locale.value = 'en'
    const { wrapper } = await mountAdminMaintenance()

    const expectedStart = new Date('2026-08-01T10:00:00Z').toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short' })
    expect(wrapper.find('tbody tr').text()).toContain(expectedStart)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ incidentsOk: false }) })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  it('zeigt bei 403 die Berechtigungsmeldung statt Tabelle und Formular', async () => {
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: vi.fn(async () => jsonResponse({}, 403)) })

    expect(wrapper.text()).toContain('Keine Berechtigung für diesen Bereich')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.edit-card').exists()).toBe(false)
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('stürzt bei einer null-Antwort nicht ab und zeigt eine leere Liste', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: vi.fn(async () => jsonResponse(null)) })

    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state__title').text()).toBe('Keine Wartungsfenster geplant.')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Keine Berechtigung')

    await wrapper.find('button').trigger('click')
    expect(wrapper.find('.edit-form').exists()).toBe(true)
    expect(wrapper.findAll('.onyx-select-stub-option')).toHaveLength(0)
  })

  it('zeigt ohne aktive Statusseite weder Berechtigungsmeldung noch Fehler', async () => {
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: vi.fn(), tenantId: null })

    expect(wrapper.text()).not.toContain('Keine Berechtigung')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('zeigt bei 500 weiterhin den Lade-Alert und nicht die Berechtigungsmeldung', async () => {
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500)) })

    expect(wrapper.find('.alert-error').text()).toContain('Fehler beim Laden')
    expect(wrapper.text()).not.toContain('Keine Berechtigung')
  })

  it('hat keine axe-Funde in Liste und Formular', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance()
    expect(await axe(wrapper)).toHaveNoViolations()

    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // /api/tags ist global-admin-only. Ein Editor sah „Keine Tags
  // vorhanden. Unter ‚Tags‘ anlegen.“, obwohl Tags existieren und er die
  // Tag-Verwaltung nicht öffnen darf. Jetzt fehlt das Feld ganz.
  it('blendet das Tag-Feld für Editoren aus und zeigt es Global Admins', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const editorFetch = makeFetchRouter()
    const { wrapper: editor } = await mountAdminMaintenance({ fetchImpl: editorFetch })
    await editor.find('button').trigger('click')
    await flushPromises()
    expect(editor.find('.edit-form').exists()).toBe(true)
    expect(editor.findAll('.edit-form .form-field__label').map(l => l.text())).not.toContain('Tags (Reichweite)')
    expect(editor.text()).not.toContain('Keine Tags vorhanden')
    expect(editorFetch.mock.calls.some(([u]) => u.includes('/api/tags'))).toBe(false)
    editor.unmount()

    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const { wrapper: admin } = await mountAdminMaintenance()
    await admin.find('button').trigger('click')
    await flushPromises()
    expect(admin.findAll('.edit-form .form-field__label').map(l => l.text())).toContain('Tags (Reichweite)')
  })
  it('Seitentitel ist eine h2 (PageHeader level 2, die Topbar trägt die h1), Karten- und Formulartitel eine h3', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance()

    expect(wrapper.findAll('h2.page-head__title').map(h => h.text())).toEqual(['Wartungsfenster'])
    expect(wrapper.find('h1').exists()).toBe(false)

    await wrapper.find('.page-head button').trigger('click')
    expect(wrapper.findAll('h2.page-head__title')).toHaveLength(1)
    expect(wrapper.find('.form-panel h3').text()).toBe('Neues Wartungsfenster planen')
    expect(wrapper.find('.comp-card h3').text()).toBe('Alle Wartungsfenster')
  })

  it('zeigt den Status als StatusBadge (Geplant, Läuft, Abgeschlossen) statt .badge', async () => {
    const items = ['scheduled', 'in_progress', 'resolved', 'seltsam'].map((Status, i) => ({
      ID: i + 1, Title: `W${i}`, Status, IsMaintenance: true, Components: null, Tags: [],
      ScheduledStartAt: '2026-08-01T10:00:00Z', ScheduledEndAt: '2026-08-01T12:00:00Z',
    }))
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ items }) })

    const badges = wrapper.findAll('tbody .st-badge')
    expect(badges.map(b => b.text())).toEqual(['Geplant', 'Läuft', 'Abgeschlossen', 'seltsam'])
    expect(badges[0].classes()).toContain('st-badge--maintenance')
    expect(badges[1].classes()).toContain('st-badge--degraded')
    expect(badges[2].classes()).toContain('st-badge--operational')
    expect(badges[3].classes()).toContain('st-badge--unknown')
    expect(wrapper.find('.badge').exists()).toBe(false)
    expect(wrapper.find('td[data-label="Komponente(n)"]').text()).toBe('—')
  })

  it('nutzt den gemeinsamen Tabellenstil und bietet „Bearbeiten“ nur für geplante Fenster an', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const items = [
      { ID: 1, Title: 'A', Status: 'scheduled', Components: [], Tags: [], ScheduledStartAt: '2026-08-01T10:00:00Z', ScheduledEndAt: '2026-08-01T12:00:00Z' },
      { ID: 2, Title: 'B', Status: 'resolved', Components: [], Tags: [], ScheduledStartAt: '2026-08-01T10:00:00Z', ScheduledEndAt: '2026-08-01T12:00:00Z' },
    ]
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ items }) })

    expect(wrapper.find('.data-table-wrapper table.data-table').exists()).toBe(true)
    const rows = wrapper.findAll('tbody tr')
    const entries = (row) => row.findAll('[role="menuitem"]').map(i => i.text())
    expect(entries(rows[0])).toEqual(['Bearbeiten', 'Löschen'])
    expect(entries(rows[1])).toEqual(['Löschen'])
  })

  it('blendet Neu-Knopf, Bearbeiten und Löschen ohne maintenance:create/update/delete aus', async () => {
    const { wrapper } = await mountAdminMaintenance()
    // Ohne Recht nur „Ansehen“, kein Neu-Knopf, kein Bearbeiten/Löschen.
    expect([...new Set(wrapper.findAll('[role="menuitem"]').map(i => i.text()))]).toEqual(['Ansehen'])
    expect(wrapper.find('.page-head button').exists()).toBe(false)
  })

  it('legt ein Wartungsfenster per POST an (Pflichtfelder mit Stern, Erinnerung in Minuten)', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })
    await wrapper.find('.page-head button').trigger('click')

    const required = wrapper.findAll('.form-field').filter(f => f.find('.form-field__required').exists())
    expect(required.map(f => f.find('label').text())).toEqual(['Titel', 'Nachricht / Details', 'Betroffene Komponente(n)', 'Start', 'Ende'])

    await fieldByLabel(wrapper, 'Titel').setValue('Neu')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Details')
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    await fieldByLabel(wrapper, 'Start').setValue('2026-10-02T08:00')
    await fieldByLabel(wrapper, 'Ende').setValue('2026-10-02T09:00')
    await fieldByLabel(wrapper, 'Erinnerung vorher (0 = keine)').setValue('2')
    await fieldByLabel(wrapper, 'Einheit').setValue('hours')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    const post = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    const body = JSON.parse(post[1].body)
    expect(body.Title).toBe('Neu')
    expect(body.IsMaintenance).toBe(true)
    expect(body.ReminderLeadMinutes).toBe(120)
    expect(body.ScheduledStartAt).toBe(new Date('2026-10-02T08:00').toISOString())
    expect(wrapper.find('.alert-success').text()).toContain('Wartungsfenster erstellt.')
    expect(wrapper.find('.edit-form').exists()).toBe(false)
  })

  it('befüllt beim Bearbeiten Felder und Erinnerung (120 Min. = 2 Stunden) und speichert per PUT', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper, 'Bearbeiten')

    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Datenbank-Upgrade')
    expect(fieldByLabel(wrapper, 'Erinnerung vorher (0 = keine)').element.value).toBe('2')
    expect(fieldByLabel(wrapper, 'Einheit').element.value).toBe('hours')
    expect(fieldByLabel(wrapper, 'Einheit').findAll('option').map(o => o.text())).toEqual(['Minuten', 'Stunden', 'Tage'])

    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents/1', expect.objectContaining({ method: 'PUT' }))
    expect(wrapper.find('.alert-success').text()).toContain('Änderungen gespeichert.')
  })

  it('übernimmt Titel, Nachricht und Komponenten aus einer Vorlage, nicht aber die Zeiten', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ templates: templatesFixture() }) })
    await wrapper.find('.page-head button').trigger('click')

    await fieldByLabel(wrapper, 'Vorlage verwenden').setValue('1')
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Datenbank-Wartung')
    expect(fieldByLabel(wrapper, 'Nachricht / Details').element.value).toBe('Kurze Unterbrechung.')
    expect(fieldByLabel(wrapper, 'Start').element.value).toBe('')

    await fieldByLabel(wrapper, 'Vorlage verwenden').setValue('')
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Datenbank-Wartung')
  })

  it('zeigt einen Speicherfehler und lässt das Formular offen', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ writeOk: false }) })
    await wrapper.find('.page-head button').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('T')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('M')
    await fieldByLabel(wrapper, 'Start').setValue('2026-10-02T08:00')
    await fieldByLabel(wrapper, 'Ende').setValue('2026-10-02T09:00')
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Speichern fehlgeschlagen')
    expect(wrapper.find('.edit-form').exists()).toBe(true)
  })

  it('hat keine Inline-Styles und keine Hex-Farben', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    const { wrapper } = await mountAdminMaintenance()
    expectNoInlineStyles(wrapper, import.meta.url, 'AdminMaintenance.vue')

    await clickMenuItem(wrapper, 'Bearbeiten')
    expectNoInlineStyles(wrapper, import.meta.url, 'AdminMaintenance.vue')
  })
  it('zeigt während des Löschens „Löschen…“ und sperrt die Schaltfläche', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    stubConfirm(true)
    const base = makeFetchRouter()
    const fetchMock = vi.fn((url, o = {}) => (o.method === 'DELETE' ? new Promise(() => {}) : base(url, o)))
    const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()
    const item = wrapper.find('[role="menuitem"]:last-child')
    expect(item.text()).toContain('Löschen…')
    expect(item.attributes('aria-disabled')).toBe('true')
  })
  // Richtung C: Seitenkopf, Kartenkopf mit Suche und Filter, Formular als Seitenkarte.
  describe('Richtung C', () => {
    const windows = () => [
      { ID: 1, Title: 'DB-Upgrade', Message: 'Kurz offline', Status: 'scheduled', Components: [{ ID: 10, Name: 'API' }], Tags: [], ScheduledStartAt: '2026-08-01T10:00:00Z', ScheduledEndAt: '2026-08-01T12:00:00Z' },
      { ID: 2, Title: 'Netzwerk-Umbau', Message: 'Router', Status: 'resolved', Components: [{ ID: 11, Name: 'Web' }], Tags: [], ScheduledStartAt: '2026-07-01T10:00:00Z', ScheduledEndAt: '2026-07-01T12:00:00Z' },
    ]

    it('zeigt Brotkrumen, Untertitel mit Zählung und kein ID-Spaltenkopf', async () => {
      useTenant().setMyPages([{ ID: 1, Name: 'Meine Seite' }])
      const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ items: windows() }) })
      expect(wrapper.find('.page-head__crumbs').text()).toContain('Meine Seite')
      expect(wrapper.find('.page-head__subtitle').text()).toBe('2 Wartungsfenster · 1 anstehend')
      expect(wrapper.find('thead').text()).not.toContain('ID')
    })

    it('sucht und filtert nach Zustand', async () => {
      const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ items: windows() }) })
      await wrapper.find('input[type="search"]').setValue('router')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      await wrapper.find('input[type="search"]').setValue('')
      const seg = (text) => wrapper.findAll('.segment__item').find(b => b.text() === text)
      await seg('Anstehend').trigger('click')
      expect(wrapper.findAll('tbody tr').map(r => r.text())).toEqual([expect.stringContaining('DB-Upgrade')])
      await seg('Abgeschlossen').trigger('click')
      expect(wrapper.findAll('tbody tr').map(r => r.text())).toEqual([expect.stringContaining('Netzwerk-Umbau')])
      await wrapper.find('input[type="search"]').setValue('gibtesnicht')
      expect(wrapper.find('.empty-state__title').text()).toBe('Kein Wartungsfenster gefunden.')
    })

    it('Formular als Seitenkarte mit Namen im Titel; Pflichtfelder zeigen Fehler am Feld ohne Request', async () => {
      setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })
      await clickMenuItem(wrapper, 'Bearbeiten')
      expect(wrapper.find('.form-panel h3').text()).toBe('Wartungsfenster „Datenbank-Upgrade“ bearbeiten')
      expect(wrapper.find('table').exists()).toBe(true)
      await wrapper.find('.form-panel__close').trigger('click')

      await wrapper.find('.page-head button').trigger('click')
      await wrapper.find('.edit-form').trigger('submit')
      await flushPromises()
      expect(wrapper.findAll('.form-field__message--error')).toHaveLength(5)
      expect(fieldByLabel(wrapper, 'Start').attributes('aria-invalid')).toBe('true')
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
      await fieldByLabel(wrapper, 'Start').setValue('2026-10-02T08:00')
      expect(fieldByLabel(wrapper, 'Start').attributes('aria-invalid')).toBeUndefined()
    })

    // Regression: Komponenten-Pflichtfeld wurde nur vom Server geprüft.
    it('Komponenten-Pflichtfeld: Fehler am Feld ohne Request, verschwindet bei Auswahl', async () => {
      setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountAdminMaintenance({ fetchImpl: fetchMock })
      await wrapper.find('.page-head button').trigger('click')
      await fieldByLabel(wrapper, 'Titel').setValue('T')
      await fieldByLabel(wrapper, 'Nachricht / Details').setValue('M')
      await fieldByLabel(wrapper, 'Start').setValue('2026-10-02T08:00')
      await fieldByLabel(wrapper, 'Ende').setValue('2026-10-02T09:00')
      await wrapper.find('.edit-form').trigger('submit')
      await flushPromises()

      expect(wrapper.findAll('.form-field__message--error').map(e => e.text())).toEqual([
        expect.stringContaining('Betroffene Komponente(n) ist ein Pflichtfeld.'),
      ])
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
      const label = wrapper.findAll('label.form-field__label').find(l => l.text() === 'Betroffene Komponente(n)')
      expect(wrapper.find(`[id="${label.attributes('for')}"]`).attributes('aria-invalid')).toBe('true')
      await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
      expect(wrapper.find('.form-field__message--error').exists()).toBe(false)
    })

    it('sucht auch in Fenstern ohne Nachricht und ohne Komponenten', async () => {
      const items = [{ ID: 3, Title: 'Nur Titel', Message: null, Status: 'scheduled', Components: null, Tags: [], ScheduledStartAt: '2026-08-01T10:00:00Z', ScheduledEndAt: '2026-08-01T12:00:00Z' }]
      const { wrapper } = await mountAdminMaintenance({ fetchImpl: makeFetchRouter({ items }) })
      await wrapper.find('input[type="search"]').setValue('titel')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      await wrapper.find('input[type="search"]').setValue('null')
      expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    })
  })

  // Ohne Recht zum Ändern öffnet „Ansehen“ das Formular gesperrt, ohne Speichern.
  it('öffnet das Formular ohne Änderungsrecht gesperrt und ohne Speichern-Knopf', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'maintenance:read' })
    const { wrapper } = await mountAdminMaintenance()
    await wrapper.findAll('[role="menuitem"]').find(i => i.text() === 'Ansehen').trigger('click')
    await flushPromises()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.form-lock-hint').text()).toContain('Nur Lesezugriff')
    expect(wrapper.findAll('button[type="submit"]')).toHaveLength(0)
  })
  // Nach dem Löschen blieb „Wartungsfenster erstellt.“ stehen, eine Löschmeldung fehlte.
  it('ersetzt beim Löschen die alte Meldung durch „Gelöscht.“ als Erfolgsmeldung', async () => {
    setAuthAsPageRole('jwt-1', { permissions: MAINTENANCE_PERMS })
    stubConfirm(true)
    const base = makeFetchRouter()
    const fetchImpl = vi.fn((url, o = {}) => (o.method === 'DELETE' ? Promise.resolve(jsonResponse({})) : base(url, o)))
    const { wrapper } = await mountAdminMaintenance({ fetchImpl })
    wrapper.vm.showAlert('Wartungsfenster erstellt.', 'success')
    await flushPromises()
    expect(wrapper.find('.alert').text()).toContain('Wartungsfenster erstellt.')

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Wartungsfenster erstellt.')
    expect(wrapper.find('.alert-success').text()).toContain('Gelöscht.')
  })
})
