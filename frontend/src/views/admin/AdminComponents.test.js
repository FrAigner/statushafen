import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminComponents from './AdminComponents.vue'
import { readFileSync } from 'node:fs'
import { axe } from '../../test-support/axe.js'
import { fieldByLabel, chooseQuickStatus } from '../../test-support/fields.js'
import { stubConfirm } from '../../test-support/confirm'

function componentsFixture() {
  return [
    { ID: 1, Name: 'API', Description: 'Kern-API', Status: 'operational', Tags: [{ ID: 5, Name: 'EU' }] },
  ]
}

function tagsFixture() {
  return [{ ID: 5, Name: 'EU' }, { ID: 6, Name: 'US' }]
}

function pageTagsFixture() {
  return [
    { ID: 5, Name: 'global-region', StatusPageID: null },
    { ID: 7, Name: 'eigen-team', StatusPageID: 1 },
  ]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ compsOk = true, tagsOk = true, writeOk = true, pageTags = pageTagsFixture() } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/components$/.test(url)) {
      return compsOk ? jsonResponse(componentsFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    // Seiten-Endpunkt für Seiten-Rollen (globale + seiteneigene Tags).
    if (method === 'GET' && /\/api\/pages\/\d+\/tags$/.test(url)) return jsonResponse(pageTags)
    if (method === 'GET' && /\/api\/tags/.test(url)) {
      return tagsOk ? jsonResponse(tagsFixture()) : jsonResponse({ error: 'tags kaputt' }, 500)
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

// Zeilenmenü: die Einträge stehen im DOM (v-show), ein Klick
// auf den Eintrag genügt; der Knopf öffnet es für Tastatur und Maus.
const menuItem = (row, text) => row.findAll('[role="menuitem"]').find(i => i.text().includes(text))
const clickMenuItem = (row, text) => menuItem(row, text).trigger('click')

async function mountComponents({ fetchImpl, perms = 'components:create,components:update,components:delete', tenantId = '1', globalAdmin = false } = {}) {
  setAuthAsPageRole('jwt-1', { permissions: perms, is_global_admin: globalAdmin })
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminComponents, {
    global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminComponents', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('verlinkt je Komponente die Übersetzungsseite', async () => {
    const { wrapper } = await mountComponents()

    const link = wrapper.findAllComponents(RouterLinkStub).find(l => l.props('to') === '/admin/components/1/translations')
    expect(link.attributes('role')).toBe('menuitem')
    expect(link.text()).toContain('Übersetzungen')
  })

  it('lädt Komponenten + Tags und zeigt Name, Beschreibung und Tags', async () => {
    const { wrapper } = await mountComponents()

    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('API')
    expect(row.text()).toContain('Kern-API')
    expect(row.text()).toContain('EU')
  })

  it('lädt Komponenten weiter, auch wenn /api/tags fehlschlägt (fehlertolerant)', async () => {
    const { wrapper } = await mountComponents({ fetchImpl: makeFetchRouter({ tagsOk: false }), globalAdmin: true })

    expect(wrapper.find('tbody tr').text()).toContain('API')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
    await wrapper.findAll('button').find(b => b.text().includes('Neue Komponente')).trigger('click')
    expect(wrapper.text()).toContain('Keine Tags vorhanden')
  })

  // /api/tags ist global-admin-only. Ohne Global Admin fragt die
  // View die Tags gar nicht erst ab und blendet das Feld aus, statt „Noch
  // „Keine Tags vorhanden. Unter ‚Tags‘ anlegen.“ zu zeigen.
  it('blendet das Tag-Feld für Seiten-Rollen ohne seiteneigene Tags aus und lädt nie /api/tags', async () => {
    const fetchMock = makeFetchRouter({ pageTags: [{ ID: 5, Name: 'global-region', StatusPageID: null }] })
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Komponente')).trigger('click')
    expect(wrapper.find('form.edit-form').exists()).toBe(true)
    expect(wrapper.find('#components-tags-label').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Keine Tags vorhanden')
    expect(fetchMock.mock.calls.some(([u]) => u.includes('/api/tags'))).toBe(false)
  })

  // Seiten-Rollen laden die Tags über den Seiten-Endpunkt und
  // bekommen nur seiteneigene Tags angeboten (globale vergibt nur ein Global Admin).
  it('zeigt Seiten-Rollen das Tag-Feld mit den seiteneigenen Tags der Seite', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Komponente')).trigger('click')
    expect(wrapper.find('#components-tags-label').exists()).toBe(true)
    expect(wrapper.find('.tag-chips__add').text()).toContain('eigen-team')
    expect(wrapper.find('.tag-chips__add').text()).not.toContain('global-region')
    expect(fetchMock.mock.calls.some(([u]) => u === '/api/pages/1/tags')).toBe(true)
    expect(fetchMock.mock.calls.some(([u]) => u.includes('/api/tags'))).toBe(false)
  })

  it('zeigt Global Admins das Tag-Feld mit den geladenen Tags', async () => {
    const { wrapper } = await mountComponents({ globalAdmin: true })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Komponente')).trigger('click')
    expect(wrapper.find('#components-tags-label').exists()).toBe(true)
    // Chips statt Checkbox-Liste; bei einer neuen Komponente sind beide Tags wählbar.
    expect(wrapper.find('.tag-chips__add').text()).toContain('EU')
    expect(wrapper.find('.tag-chips__add').text()).toContain('US')
  })

  it('zeigt eine Fehlermeldung, wenn das Laden der Komponenten fehlschlägt', async () => {
    const { wrapper } = await mountComponents({ fetchImpl: makeFetchRouter({ compsOk: false }) })

    expect(wrapper.find('.alert-error').text()).toContain('kaputt')
  })

  it('ändert den Status per Schnellstatus über PUT (quickStatus)', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })

    await chooseQuickStatus(wrapper.find('.quick-status__button'), 'Großstörung')
    await flushPromises()

    const [url, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'PUT')
    expect(url).toContain('/components/1')
    expect(JSON.parse(options.body)).toEqual({ status: 'major_outage' })
    // Rückmeldung nach dem Inline-Statuswechsel.
    expect(wrapper.find('.alert-success').text()).toContain('Änderungen gespeichert')
  })

  // Die API liefert einen Baum; vorher listete der Admin nur die
  // oberste Ebene, Unterkomponenten waren weder sichtbar noch bearbeitbar.
  it('listet Unterkomponenten eingerückt in Baumreihenfolge und macht sie bearbeitbar', async () => {
    const tree = [
      {
        ID: 1, Name: 'Core', Status: 'operational', Tags: [], Children: [
          { ID: 4, Name: 'API Services', Status: 'operational', Tags: [], Children: [
            { ID: 7, Name: 'API Gateway (US)', Description: 'US East', Status: 'degraded_performance', Tags: [] },
          ] },
        ],
      },
      { ID: 2, Name: 'Extern', Status: 'operational', Tags: [] },
    ]
    const base = makeFetchRouter()
    const fetchMock = vi.fn(async (url, options = {}) =>
      (!options.method && /\/components$/.test(url)) ? jsonResponse(tree) : base(url, options))
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })

    const rows = wrapper.findAll('tbody tr')
    expect(rows.map(r => r.find('strong').text())).toEqual(['Core', 'API Services', 'API Gateway (US)', 'Extern'])
    expect(rows.map(r => r.find('.component-name').attributes('data-depth'))).toEqual(['0', '1', '2', '0'])
    expect(rows[2].find('.component-name').classes()).toContain('depth-2')

    await clickMenuItem(rows[2], 'Bearbeiten')
    expect(fieldByLabel(wrapper, 'Name').element.value).toBe('API Gateway (US)')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    const [url] = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(url).toContain('/components/7')
  })

  // Die API ersetzt den Status einer Gruppe durch den schlechtesten
  // Kindstatus. Der Admin bot dafür einen Status-Select an und schickte den
  // Wert beim Speichern mit; das Backend machte aus der Gruppe dann eine
  // normale Komponente.
  describe('Gruppen', () => {
    const groupTree = () => [
      { ID: 1, Name: 'Core', Status: 'major_outage', IsGroup: true, Tags: [], Children: [
        { ID: 3, Name: 'DB', Status: 'major_outage', IsGroup: false, Tags: [] },
      ] },
      { ID: 2, Name: 'Leer', Status: 'group', IsGroup: true, Tags: [] },
    ]
    const mountWithGroups = async () => {
      const base = makeFetchRouter()
      const fetchMock = vi.fn(async (url, options = {}) =>
        (!options.method && /\/components$/.test(url)) ? jsonResponse(groupTree()) : base(url, options))
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      return { wrapper, fetchMock }
    }

    it('zeigt für Gruppen keinen Schnellstatus, nur den berechneten Status', async () => {
      const { wrapper } = await mountWithGroups()
      const rows = wrapper.findAll('tbody tr')

      expect(rows[0].find('.quick-status').exists()).toBe(false)
      expect(rows[0].find('.st-badge--major').exists()).toBe(true)
      expect(rows[0].find('td .sr-only').text()).toBe('Gruppe')
      // Gruppe ohne Kinder: kein Roh-Badge "group", nur das Gruppen-Label.
      expect(rows[2].find('.st-badge').exists()).toBe(false)
      expect(rows[2].find('td .sr-only').exists()).toBe(true)
      // Normale Komponente behält den Schnellstatus.
      expect(rows[1].find('.quick-status').exists()).toBe(true)
      expect(rows[1].find('td .sr-only').exists()).toBe(false)
    })

    it('bietet im Formular keinen Status an und schickt beim Speichern keinen mit', async () => {
      const { wrapper, fetchMock } = await mountWithGroups()

      await clickMenuItem(wrapper.findAll('tbody tr')[0], 'Bearbeiten')
      expect(wrapper.find('fieldset legend').exists()).toBe(false)
      expect(wrapper.find('.group-status-hint').text()).toContain('Unterkomponenten')

      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      const [url, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
      expect(url).toContain('/components/1')
      const body = JSON.parse(options.body)
      expect(body).not.toHaveProperty('status')
      expect(body.name).toBe('Core')
    })

    it('schickt bei normalen Komponenten weiter den Status mit', async () => {
      const { wrapper, fetchMock } = await mountWithGroups()

      await clickMenuItem(wrapper.findAll('tbody tr')[1], 'Bearbeiten')
      expect(wrapper.find('.group-status-hint').exists()).toBe(false)
      expect(wrapper.find('fieldset legend').text()).toContain('Status')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
      expect(JSON.parse(options.body).status).toBe('major_outage')
    })
  })

  it('legt eine neue Komponente per POST mit gewählten Tags an', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock, globalAdmin: true })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Komponente')).trigger('click')
    await fieldByLabel(wrapper, 'Name').setValue('Web')
    await wrapper.find('.tag-chips__add').setValue('5')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const [url, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST')
    expect(url).toContain('/components')
    expect(JSON.parse(options.body)).toMatchObject({ name: 'Web', status: 'operational', tag_ids: [5] })
    expect(wrapper.find('.alert-success').text()).toContain('Komponente erstellt.')
  })

  it('lädt eine bestehende Komponente zum Bearbeiten inkl. TagIDs aus Tags', async () => {
    const { wrapper } = await mountComponents({ globalAdmin: true })

    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')

    expect(fieldByLabel(wrapper, 'Name').element.value).toBe('API')
    expect(wrapper.find('.tag-chip').text()).toContain('EU')
  })

  it('übernimmt Beschreibung und Status aus dem Bearbeiten-Formular per PUT', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')
    await fieldByLabel(wrapper, 'Beschreibung').setValue('Öffentliche REST-API')
    await wrapper.findAll('.status-tiles__input').find(i => i.element.value === 'partial_outage').setValue(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(JSON.parse(options.body)).toMatchObject({ description: 'Öffentliche REST-API', status: 'partial_outage' })
  })

  it('schließt eine Meldung über das ✕', async () => {
    const { wrapper } = await mountComponents({ fetchImpl: makeFetchRouter({ compsOk: false }) })
    expect(wrapper.find('.alert-error').exists()).toBe(true)

    await wrapper.find('.alert-close').trigger('click')

    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('löscht eine Komponente nach Bestätigung per DELETE', async () => {
    stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper.find('tbody tr'), 'Löschen')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/components/1'), expect.objectContaining({ method: 'DELETE' }))
  })

  it('zeigt ein reines Status-Badge statt Inline-Select ohne components:update-Recht', async () => {
    const { wrapper } = await mountComponents({ perms: 'components:read' })

    expect(wrapper.find('.quick-status').exists()).toBe(false)
    expect(wrapper.find('.st-badge').exists()).toBe(true)
    // Leser bekommen „Ansehen“ und die Übersetzungen, kein Bearbeiten/Löschen.
    expect(wrapper.findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Ansehen', 'Übersetzungen'])
  })

  it('zeigt bei 403 die Berechtigungsmeldung statt Tabelle und Formular', async () => {
    const { wrapper } = await mountComponents({ fetchImpl: vi.fn(async () => jsonResponse({}, 403)) })

    expect(wrapper.text()).toContain('Keine Berechtigung für diesen Bereich')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.edit-card').exists()).toBe(false)
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('stürzt bei einer null-Antwort nicht ab und zeigt eine leere Liste', async () => {
    const { wrapper } = await mountComponents({ fetchImpl: vi.fn(async () => jsonResponse(null)) })

    expect(wrapper.find('tbody').exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.find('.alert-error').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Keine Berechtigung')
  })

  it('zeigt ohne aktive Statusseite weder Berechtigungsmeldung noch Fehler', async () => {
    const { wrapper } = await mountComponents({ fetchImpl: vi.fn(), tenantId: null })

    expect(wrapper.text()).not.toContain('Keine Berechtigung')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('zeigt bei 500 weiterhin den Lade-Alert und nicht die Berechtigungsmeldung', async () => {
    const { wrapper } = await mountComponents({ fetchImpl: vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500)) })

    expect(wrapper.find('.alert-error').text()).toContain('Fehler beim Laden')
    expect(wrapper.text()).not.toContain('Keine Berechtigung')
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    const { wrapper } = await mountComponents()
    expect(await axe(wrapper)).toHaveNoViolations()

    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Stufe 4c: Das Formular ersetzte früher die Liste.
  it('zeigt Liste und Formular nebeneinander (Regression: Formular verdrängte die Tabelle)', async () => {
    const { wrapper } = await mountComponents()
    expect(wrapper.find('.split').classes()).not.toContain('split--open')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)

    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')

    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('table.data-table').exists()).toBe(true)
    expect(wrapper.find('aside.form-panel').attributes('aria-label')).toBe('Komponente „API“ bearbeiten')
    // Pflichtfeld über FormField: Stern nur in der Darstellung, nicht im Label-Text.
    expect(wrapper.find('.form-field__required').exists()).toBe(true)

    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  it('zeigt Gruppen als Gruppenzeile mit Zeilenkopf und alle Komponenten mit Status-Badge', async () => {
    const base = makeFetchRouter()
    const tree = [{ ID: 1, Name: 'Core', Status: 'degraded_performance', IsGroup: true, Tags: [], Children: [] }]
    const fetchMock = vi.fn(async (url, options = {}) =>
      (!options.method && /\/components$/.test(url)) ? jsonResponse(tree) : base(url, options))
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock, perms: 'components:read' })

    const row = wrapper.find('tbody tr')
    expect(row.classes()).toContain('data-table__group')
    expect(row.find('th[scope="row"]').text()).toContain('Core')
    expect(row.find('.st-badge--degraded').exists()).toBe(true)
  })

  // Gruppen sollen sich klar von Komponenten absetzen (Symbol + getönte Fläche).
  it('kennzeichnet nur Gruppenzeilen mit Gruppen-Symbol', async () => {
    const base = makeFetchRouter()
    const tree = [{ ID: 1, Name: 'Core', Status: 'operational', IsGroup: true, Tags: [], Children: [{ ID: 2 }] },
      { ID: 2, Name: 'API', Status: 'operational', ParentID: 1, Tags: [], Children: [] }]
    const fetchMock = vi.fn(async (url, options = {}) =>
      (!options.method && /\/components$/.test(url)) ? jsonResponse(tree) : base(url, options))
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock, perms: 'components:read' })

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('.group-icon').exists()).toBe(true)
    expect(rows[1].find('.group-icon').exists()).toBe(false)
  })

  it('setzt die Gruppenzeile per Token-Fläche von Komponentenzeilen ab (kein Hex)', () => {
    const styleSource = readFileSync('src/style.css', 'utf8')
    const rule = styleSource.match(/\.data-table tbody tr\.data-table__group \{([^}]*)\}/)[1]
    expect(rule).toMatch(/background:\s*var\(--primary-soft\)/)
    expect(styleSource).toMatch(/tr\.data-table__group > :first-child \{[^}]*inset 3px 0 0 var\(--primary\)/)
  })

  it('der Schnellstatus ist aus der Liste mit dem Komponentennamen benannt', async () => {
    const { wrapper } = await mountComponents()
    expect(wrapper.find('.quick-status__button').attributes('aria-label')).toBe('Status von API')
  })

  // Richtung C: Seitenkopf, Karte, Gruppen, Suche, Formular.
  describe('Richtung C', () => {
    const ago = days => new Date(Date.now() - days * 86400000).toISOString()
    const tree = () => [
      { ID: 1, Name: 'Kerndienste', Status: 'degraded_performance', IsGroup: true, Tags: [], UpdatedAt: ago(3), Children: [
        { ID: 3, Name: 'Website', Description: 'www.acme.de', Status: 'operational', Tags: [{ ID: 5, Name: 'EU' }, { ID: 6, Name: 'US' }], UpdatedAt: ago(3), ParentID: 1 },
        { ID: 4, Name: 'API', Status: 'degraded_performance', Tags: [], UpdatedAt: ago(3), ParentID: 1 },
      ] },
      { ID: 2, Name: 'Webhooks', Status: 'operational', Tags: [], UpdatedAt: ago(3) },
    ]
    const routerFor = (body) => {
      const base = makeFetchRouter()
      return vi.fn(async (url, options = {}) =>
        (!options.method && /\/components$/.test(url)) ? jsonResponse(body) : base(url, options))
    }
    const mountTree = async (opts = {}) => {
      const fetchMock = routerFor(tree())
      useTenant().setMyPages([{ ID: 1, Name: 'Acme Corp' }])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock, ...opts })
      return { wrapper, fetchMock }
    }
    const names = wrapper => wrapper.findAll('tbody tr').map(r => r.find('strong').text())
    const newButton = wrapper => wrapper.findAll('button').find(b => b.text().includes('Neue Komponente'))

    it('zeigt Brotkrumen mit dem Seitennamen, h2-Titel und Untertitel mit Zählung', async () => {
      const { wrapper } = await mountTree()
      expect(wrapper.find('h2').text()).toBe('Komponenten')
      expect(wrapper.find('.page-head__crumbs').text()).toContain('Acme Corp')
      expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe('Komponenten')
      expect(wrapper.find('.page-head__subtitle').text()).toBe('3 Komponenten in 1 Gruppe · Statusänderungen sind sofort öffentlich.')
    })

    it('zeigt Brotkrumen ohne bekannten Seitennamen nur mit der aktuellen Seite', async () => {
      useTenant().setMyPages([])
      const { wrapper } = await mountComponents()
      expect(wrapper.findAll('.page-head__crumbs li')).toHaveLength(1)
    })

    it('zeigt Spalten Name, Tags, Status (Schnellwechsel), Geändert und eine schmale Menüspalte', async () => {
      const { wrapper } = await mountTree()
      expect(wrapper.findAll('thead th').map(th => th.text())).toEqual(['Name', 'Tags', 'Status (Schnellwechsel)', 'Geändert', 'Aktionen'])
      const row = wrapper.findAll('tbody tr')[1]
      expect(row.find('strong').text()).toBe('Website')
      expect(row.find('.component-name__desc').text()).toBe('www.acme.de')
      expect(row.findAll('.tag-pill').map(p => p.text())).toEqual(['EU', 'US'])
      expect(row.find('time').text()).toBe('vor 3 Tagen')
      expect(row.find('time').attributes('datetime')).toBeTruthy()
      expect(row.find('.quick-status').exists()).toBe(true)
    })

    it('zeigt einen Strich ohne Tags und ohne Änderungsdatum', async () => {
      const fetchMock = routerFor([{ ID: 1, Name: 'X', Status: 'operational' }])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      const cells = wrapper.findAll('tbody tr td')
      expect(cells[1].text()).toBe('—')
      expect(cells[3].text()).toBe('—')
      // Die Suche verträgt fehlende Tags (die API liefert dann null).
      await wrapper.find('input[type="search"]').setValue('x')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    })

    it('klappt Gruppen ein und aus (Anzeige, aria-expanded, Beschriftung)', async () => {
      const { wrapper } = await mountTree()
      expect(names(wrapper)).toEqual(['Kerndienste', 'Website', 'API', 'Webhooks'])
      const toggle = wrapper.find('.group-toggle')
      expect(toggle.attributes('aria-expanded')).toBe('true')
      expect(toggle.attributes('aria-label')).toBe('Gruppe „Kerndienste“ einklappen')
      expect(wrapper.find('.group-count').text()).toBe('2')

      await toggle.trigger('click')
      expect(names(wrapper)).toEqual(['Kerndienste', 'Webhooks'])
      expect(toggle.attributes('aria-expanded')).toBe('false')
      expect(toggle.attributes('aria-label')).toBe('Gruppe „Kerndienste“ ausklappen')

      await toggle.trigger('click')
      expect(names(wrapper)).toEqual(['Kerndienste', 'Website', 'API', 'Webhooks'])
    })

    it('sucht nach Name, Beschreibung und Tag, zeigt Treffer samt Gruppe und sagt die Trefferzahl an', async () => {
      const { wrapper } = await mountTree()
      const search = wrapper.find('input[type="search"]')
      expect(search.attributes('aria-label')).toBe('Komponente suchen')

      await search.setValue('acme.de')
      expect(names(wrapper)).toEqual(['Kerndienste', 'Website'])
      expect(wrapper.find('[role="status"]').text()).toBe('2 Treffer')

      await search.setValue('webh')
      expect(names(wrapper)).toEqual(['Webhooks'])
      expect(wrapper.find('[role="status"]').text()).toBe('1 Treffer')

      await search.setValue('EU')
      expect(names(wrapper)).toEqual(['Kerndienste', 'Website'])
    })

    it('zeigt bei der Suche trotz eingeklappter Gruppe den Treffer und danach wieder den alten Zustand', async () => {
      const { wrapper } = await mountTree()
      await wrapper.find('.group-toggle').trigger('click')
      await wrapper.find('input[type="search"]').setValue('api')
      expect(names(wrapper)).toEqual(['Kerndienste', 'API'])
      expect(wrapper.find('.group-toggle').attributes('aria-expanded')).toBe('true')
      await wrapper.find('input[type="search"]').setValue('')
      expect(names(wrapper)).toEqual(['Kerndienste', 'Webhooks'])
    })

    it('zeigt ohne Treffer einen Leerzustand statt der Tabelle', async () => {
      const { wrapper } = await mountTree()
      await wrapper.find('input[type="search"]').setValue('gibtesnicht')
      expect(wrapper.find('table').exists()).toBe(false)
      expect(wrapper.find('.empty-state__title').text()).toBe('Keine Komponente gefunden.')
    })

    it('öffnet das Zeilenmenü mit Bearbeiten, Übersetzungen und Löschen (Tastatur: Pfeil unten)', async () => {
      const { wrapper } = await mountTree()
      const row = wrapper.findAll('tbody tr')[1]
      const button = row.find('.row-menu__button')
      expect(button.attributes('aria-label')).toBe('Aktionen für „Website“')
      await button.trigger('keydown', { key: 'ArrowDown' })
      await flushPromises()
      expect(button.attributes('aria-expanded')).toBe('true')
      expect(row.findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Bearbeiten', 'Übersetzungen', 'Archivieren', 'Löschen'])
    })

    it('bearbeitet über das Menü: Titel mit Namen, bleibt beim Tippen stabil', async () => {
      const { wrapper } = await mountTree()
      await clickMenuItem(wrapper.findAll('tbody tr')[1], 'Bearbeiten')
      await flushPromises()
      const title = () => wrapper.find('.form-panel h3').text()
      expect(title()).toBe('Komponente „Website“ bearbeiten')
      await fieldByLabel(wrapper, 'Name').setValue('Website neu')
      expect(title()).toBe('Komponente „Website“ bearbeiten')
      expect(wrapper.find('.form-panel__hint').text()).toBe('Felder mit * sind Pflichtfelder.')
    })

    // Ohne data-self-validates fängt der Submit-Wächter (utils/formValidation.js) den
    // Submit mit leerem Pflichtfeld ab und zeigt nur den englischen Browser-Hinweis, die
    // Feldmeldung der View erscheint nie (im Browser; happy-dom installiert den Wächter nicht).
    it('Formular meldet sich beim Submit-Wächter als selbstprüfend an', async () => {
      const { wrapper } = await mountComponents()
      await newButton(wrapper).trigger('click')
      expect(wrapper.find('form.edit-form').attributes('data-self-validates')).toBeDefined()
    })

    it('Pflichtfeld Name: zeigt den Fehler am Feld, sendet nichts und räumt ihn beim Tippen ab', async () => {
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await newButton(wrapper).trigger('click')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()

      const name = fieldByLabel(wrapper, 'Name')
      expect(wrapper.find('.form-field__message--error').text()).toContain('Bitte einen Namen eingeben.')
      expect(name.attributes('aria-invalid')).toBe('true')
      expect(name.attributes('aria-describedby')).toBeTruthy()
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)

      await name.setValue('Neu')
      expect(wrapper.find('.form-field__message--error').exists()).toBe(false)
    })

    it('Pflichtfeld Name: nur Leerzeichen zählen nicht', async () => {
      const { wrapper } = await mountComponents()
      await newButton(wrapper).trigger('click')
      await fieldByLabel(wrapper, 'Name').setValue('   ')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      expect(wrapper.find('.form-field__message--error').exists()).toBe(true)
    })

    it('schließt das Formular mit dem ×-Knopf, ohne zu speichern', async () => {
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await newButton(wrapper).trigger('click')
      const close = wrapper.find('.form-panel__close')
      expect(close.attributes('aria-label')).toBe('Formular schließen')
      await close.trigger('click')
      expect(wrapper.find('aside.form-panel').exists()).toBe(false)
      expect(fetchMock.mock.calls.some(([, o]) => ['POST', 'PUT'].includes(o?.method))).toBe(false)
    })

    it('legt mit gewähltem Anfangsstatus und Gruppe an (parent_id)', async () => {
      const { wrapper, fetchMock } = await mountTree()
      await newButton(wrapper).trigger('click')
      expect(wrapper.find('fieldset legend').text()).toContain('Anfangsstatus')
      await fieldByLabel(wrapper, 'Name').setValue('Zahlung')
      await wrapper.findAll('.status-tiles__input').find(i => i.element.value === 'major_outage').setValue(true)
      await fieldByLabel(wrapper, 'Gruppe').findAll('option')[1].setSelected()
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
      expect(JSON.parse(options.body)).toMatchObject({ name: 'Zahlung', status: 'major_outage', parent_id: 1 })
    })

    it('sendet ohne Gruppe parent_id 0 und beim Bearbeiten die bestehende Gruppe unverändert', async () => {
      const { wrapper, fetchMock } = await mountTree()
      await clickMenuItem(wrapper.findAll('tbody tr')[3], 'Bearbeiten')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      const puts = () => fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT')
      expect(JSON.parse(puts()[0][1].body).parent_id).toBe(0)

      await clickMenuItem(wrapper.findAll('tbody tr')[1], 'Bearbeiten')
      expect(fieldByLabel(wrapper, 'Gruppe').element.value).toBe('1')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(JSON.parse(puts().at(-1)[1].body).parent_id).toBe(1)
    })

    it('bietet als Gruppe nur Gruppen an, nie die Komponente selbst oder ihre Nachfahren', async () => {
      const { wrapper } = await mountTree()
      await newButton(wrapper).trigger('click')
      const optionTexts = () => fieldByLabel(wrapper, 'Gruppe').findAll('option').map(o => o.text())
      expect(optionTexts()).toEqual(['Keine Gruppe', 'Kerndienste'])

      await wrapper.find('.form-panel__close').trigger('click')
      await clickMenuItem(wrapper.findAll('tbody tr')[0], 'Bearbeiten')
      expect(optionTexts()).toEqual(['Keine Gruppe'])
    })

    it('behält eine Eltern-Komponente, die keine Gruppe ist, als wählbaren Eintrag', async () => {
      const nested = [{ ID: 1, Name: 'Eltern', Status: 'operational', Tags: [], Children: [{ ID: 2, Name: 'Kind', Status: 'operational', Tags: [], ParentID: 1 }] }]
      const { wrapper } = await mountComponents({ fetchImpl: routerFor(nested) })
      await clickMenuItem(wrapper.findAll('tbody tr')[1], 'Bearbeiten')
      expect(fieldByLabel(wrapper, 'Gruppe').findAll('option').map(o => o.text())).toEqual(['Keine Gruppe', 'Eltern'])
    })

    it('Berechtigungen: ohne Recht weder Neu-Knopf noch Zeilenmenü noch Schnellstatus', async () => {
      const { wrapper } = await mountTree({ perms: 'components:read' })
      expect(newButton(wrapper)).toBeUndefined()
      expect(wrapper.find('.quick-status').exists()).toBe(false)
      expect(wrapper.findAll('thead th').map(th => th.text())).toEqual(['Name', 'Tags', 'Status (Schnellwechsel)', 'Geändert', 'Aktionen'])
      expect(wrapper.findAll('[role="menuitem"]').every(i => ['Ansehen', 'Übersetzungen'].includes(i.text()))).toBe(true)
      // Lesen und Suchen bleiben möglich.
      await wrapper.find('input[type="search"]').setValue('webh')
      expect(names(wrapper)).toEqual(['Webhooks'])
    })

    it('Mandantentrennung: lädt die Komponenten der aktiven Statusseite', async () => {
      const { fetchMock } = await mountTree({ tenantId: '7' })
      expect(fetchMock.mock.calls.some(([u]) => u === '/api/pages/7/components')).toBe(true)
    })

    // Gruppen anlegen und das Löschen nicht leerer Gruppen verhindern.
    describe('Gruppen', () => {
      const groupButton = wrapper => wrapper.findAll('button').find(b => b.text().includes('Neue Gruppe'))
      const postBody = fetchMock => JSON.parse(fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')[1].body)
      const emptyGroup = () => [{ ID: 9, Name: 'Leer', Status: 'group', IsGroup: true, Tags: [], Children: [] }]

      it('legt über „Neue Gruppe“ ohne Status an und schickt is_group, Name, Beschreibung, Eltern-Gruppe', async () => {
        const { wrapper, fetchMock } = await mountTree({ globalAdmin: true })
        await groupButton(wrapper).trigger('click')
        expect(wrapper.find('aside.form-panel').attributes('aria-label')).toBe('Neue Gruppe anlegen')
        expect(wrapper.find('fieldset legend').exists()).toBe(false)
        expect(wrapper.find('.group-status-hint').exists()).toBe(true)
        await fieldByLabel(wrapper, 'Name').setValue('Zahlungen')
        await fieldByLabel(wrapper, 'Beschreibung').setValue('alles rund ums Geld')
        await fieldByLabel(wrapper, 'Gruppe').findAll('option')[1].setSelected()
        await wrapper.find('form.edit-form').trigger('submit.prevent')
        await flushPromises()
        const body = postBody(fetchMock)
        expect(body).toMatchObject({ name: 'Zahlungen', description: 'alles rund ums Geld', parent_id: 1, is_group: true })
        expect(body).not.toHaveProperty('status')
        expect(wrapper.find('.alert-success').text()).toContain('Gruppe erstellt.')
        expect(wrapper.find('aside.form-panel').exists()).toBe(false)
      })

      it('normale Komponenten schicken einen Status und kein is_group', async () => {
        const { wrapper, fetchMock } = await mountTree()
        await newButton(wrapper).trigger('click')
        await fieldByLabel(wrapper, 'Name').setValue('Normal')
        await wrapper.find('form.edit-form').trigger('submit.prevent')
        await flushPromises()
        const body = postBody(fetchMock)
        expect(body.status).toBe('operational')
        expect(body).not.toHaveProperty('is_group')
        expect(wrapper.find('.alert-success').text()).toContain('Komponente erstellt.')
      })

      it('verlangt auch bei Gruppen einen Namen', async () => {
        const { wrapper, fetchMock } = await mountTree()
        await groupButton(wrapper).trigger('click')
        await wrapper.find('form.edit-form').trigger('submit.prevent')
        expect(wrapper.find('.form-field__message--error').text()).toContain('Bitte einen Namen eingeben.')
        expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
      })

      it('zeigt den Serverfehler beim Anlegen einer Gruppe', async () => {
        const base = routerFor(tree())
        const failing = vi.fn(async (url, o = {}) => o.method === 'POST' ? jsonResponse({ error: 'kaputt' }, 500) : base(url, o))
        const { wrapper } = await mountComponents({ fetchImpl: failing })
        await groupButton(wrapper).trigger('click')
        await fieldByLabel(wrapper, 'Name').setValue('X')
        await wrapper.find('form.edit-form').trigger('submit.prevent')
        await flushPromises()
        expect(wrapper.find('.alert-error').text()).toContain('kaputt')
      })

      it('Bearbeiten einer Gruppe: Titel „Gruppe … bearbeiten“, kein Status, PUT ohne status', async () => {
        const { wrapper, fetchMock } = await mountTree()
        await clickMenuItem(wrapper.findAll('tbody tr')[0], 'Bearbeiten')
        expect(wrapper.find('aside.form-panel').attributes('aria-label')).toBe('Gruppe „Kerndienste“ bearbeiten')
        expect(wrapper.find('fieldset legend').exists()).toBe(false)
        await wrapper.find('form.edit-form').trigger('submit.prevent')
        await flushPromises()
        const put = JSON.parse(fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')[1].body)
        expect(put).not.toHaveProperty('status')
        expect(put).not.toHaveProperty('is_group')
      })

      it('Löschen ist bei einer Gruppe mit Mitgliedern gesperrt und sendet nichts', async () => {
        stubConfirm(true)
        const { wrapper, fetchMock } = await mountTree()
        const item = menuItem(wrapper.findAll('tbody tr')[0], 'Löschen')
        expect(item.text()).toBe('Löschen (Gruppe nicht leer)')
        expect(item.attributes('aria-disabled')).toBe('true')
        await item.trigger('click')
        await flushPromises()
        expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)
      })

      it('Löschen ist bei einer leeren Gruppe möglich', async () => {
        stubConfirm(true)
        const fetchMock = routerFor(emptyGroup())
        const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
        const item = menuItem(wrapper.findAll('tbody tr')[0], 'Löschen')
        expect(item.text()).toBe('Löschen')
        await item.trigger('click')
        await flushPromises()
        expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/components/9'), expect.objectContaining({ method: 'DELETE' }))
      })

      it('übersetzt den 409 group_not_empty des Servers (Gruppe zwischenzeitlich befüllt)', async () => {
        stubConfirm(true)
        const base = routerFor(emptyGroup())
        const conflict = vi.fn(async (url, o = {}) => o.method === 'DELETE'
          ? jsonResponse({ error: 'Group is not empty: move or delete its components first', code: 'group_not_empty' }, 409)
          : base(url, o))
        const { wrapper } = await mountComponents({ fetchImpl: conflict })
        await clickMenuItem(wrapper.findAll('tbody tr')[0], 'Löschen')
        await flushPromises()
        expect(wrapper.find('.alert-error').text()).toBe('Die Gruppe ist nicht leer. Verschiebe oder lösche zuerst ihre Komponenten.')
      })

      it('zeigt bei anderen Löschfehlern die Server-Meldung', async () => {
        stubConfirm(true)
        const base = routerFor(emptyGroup())
        const failing = vi.fn(async (url, o = {}) => o.method === 'DELETE' ? jsonResponse({ error: 'kaputt' }, 500) : base(url, o))
        const { wrapper } = await mountComponents({ fetchImpl: failing })
        await clickMenuItem(wrapper.findAll('tbody tr')[0], 'Löschen')
        await flushPromises()
        expect(wrapper.find('.alert-error').text()).toContain('kaputt')
      })

      it('ohne components:create gibt es keinen „Neue Gruppe“-Knopf', async () => {
        const { wrapper } = await mountTree({ perms: 'components:read' })
        expect(groupButton(wrapper)).toBeUndefined()
      })
    })

    it('hat in Liste, offenem Zeilenmenü und Formular keine axe-Funde', async () => {
      const { wrapper } = await mountTree({ globalAdmin: true })
      expect(await axe(wrapper)).toHaveNoViolations()
      await wrapper.findAll('tbody tr')[1].find('.row-menu__button').trigger('click')
      await flushPromises()
      expect(await axe(wrapper)).toHaveNoViolations()
      await newButton(wrapper).trigger('click')
      await flushPromises()
      expect(await axe(wrapper)).toHaveNoViolations()
    })
  })
})

// Feldlängen (Name 60, Beschreibung 240) und Sichtbarkeitsschalter.
describe('AdminComponents Feldregeln', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  const withComponents = (list, writeImpl) => {
    const base = makeFetchRouter()
    return vi.fn(async (url, options = {}) => {
      if (!options.method && /\/components$/.test(url)) return jsonResponse(list)
      if (writeImpl && ['POST', 'PUT'].includes(options.method)) return writeImpl(url, options)
      return base(url, options)
    })
  }
  const lastWrite = (fetchMock, method) => fetchMock.mock.calls.filter(([, o]) => o?.method === method).at(-1)
  const visibleSwitch = (wrapper) => wrapper.find('[role="group"][aria-label="Auf der Statusseite anzeigen"] input')

  it('begrenzt die Felder per maxlength und zählt Zeichen mit', async () => {
    const { wrapper } = await mountComponents()
    await wrapper.findAll('button').find(b => b.text().includes('Neue Komponente')).trigger('click')

    expect(fieldByLabel(wrapper, 'Name').attributes('maxlength')).toBe('60')
    expect(fieldByLabel(wrapper, 'Beschreibung').attributes('maxlength')).toBe('240')
    expect(wrapper.text()).toContain('0 / 60 Zeichen')
    expect(wrapper.text()).toContain('0 / 240 Zeichen')
    await fieldByLabel(wrapper, 'Name').setValue('Größe')
    await fieldByLabel(wrapper, 'Beschreibung').setValue('😀😀')
    expect(wrapper.text()).toContain('5 / 60 Zeichen')
    expect(wrapper.text()).toContain('2 / 240 Zeichen')
  })

  it('meldet einen bestehenden zu langen Text sofort am Feld und speichert nicht', async () => {
    const fetchMock = withComponents([{ ID: 1, Name: 'n'.repeat(70), Description: 'd'.repeat(300), Status: 'operational', Tags: [], Visible: true }])
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')

    expect(fieldByLabel(wrapper, 'Name').attributes('aria-invalid')).toBe('true')
    expect(fieldByLabel(wrapper, 'Beschreibung').attributes('aria-invalid')).toBe('true')
    expect(wrapper.text()).toContain('höchstens 60 Zeichen')
    expect(wrapper.text()).toContain('höchstens 240 Zeichen')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(lastWrite(fetchMock, 'PUT')).toBeUndefined()

    await fieldByLabel(wrapper, 'Name').setValue('kurz')
    await fieldByLabel(wrapper, 'Beschreibung').setValue('auch kurz')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(lastWrite(fetchMock, 'PUT')).toBeDefined()
  })

  it.each([
    ['name_too_long', 'Name'],
    ['description_too_long', 'Beschreibung'],
  ])('zeigt den Serverfehler %s am Feld statt als allgemeine Meldung', async (code, label) => {
    const fetchMock = withComponents(
      [{ ID: 1, Name: 'API', Status: 'operational', Tags: [], Visible: true }],
      () => jsonResponse({ error: 'zu lang', code }, 400),
    )
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(fieldByLabel(wrapper, label).attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('zeigt sonstige Speicherfehler weiterhin als Meldung', async () => {
    const fetchMock = withComponents(
      [{ ID: 1, Name: 'API', Status: 'operational', Tags: [], Visible: true }],
      () => jsonResponse({ error: 'kaputt' }, 500),
    )
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('kaputt')
  })

  it('der Sichtbarkeitsschalter ist beim Anlegen an und sendet visible', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neue Komponente')).trigger('click')

    expect(visibleSwitch(wrapper).element.checked).toBe(true)
    expect(wrapper.text()).toContain('Besucher sehen die Komponente')
    await fieldByLabel(wrapper, 'Name').setValue('Intern')
    await visibleSwitch(wrapper).setValue(false)
    expect(wrapper.text()).toContain('öffentlich ausgeblendet')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(JSON.parse(lastWrite(fetchMock, 'POST')[1].body).visible).toBe(false)
  })

  it('kennzeichnet unsichtbare Komponenten in der Liste und lädt den Schalter beim Bearbeiten aus', async () => {
    const fetchMock = withComponents([
      { ID: 1, Name: 'Offen', Status: 'operational', Tags: [], Visible: true },
      { ID: 2, Name: 'Geheim', Status: 'operational', Tags: [], Visible: false },
    ])
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).not.toContain('Nicht öffentlich')
    expect(rows[1].text()).toContain('Nicht öffentlich')

    await clickMenuItem(rows[1], 'Bearbeiten')
    expect(visibleSwitch(wrapper).element.checked).toBe(false)
    await visibleSwitch(wrapper).setValue(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(JSON.parse(lastWrite(fetchMock, 'PUT')[1].body).visible).toBe(true)
  })

  it('behandelt ein fehlendes Visible (Altstand der API) als sichtbar', async () => {
    // Weder Visible noch Tags im Datensatz: sichtbar, leere Tag-Auswahl.
    const fetchMock = withComponents([{ ID: 1, Name: 'API', Status: 'operational' }])
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
    expect(wrapper.find('tbody tr').text()).not.toContain('Nicht öffentlich')
    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')
    expect(visibleSwitch(wrapper).element.checked).toBe(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(JSON.parse(lastWrite(fetchMock, 'PUT')[1].body)).toMatchObject({ visible: true, tag_ids: [] })
  })

  it('hat im Formular mit Zähler, Fehler und Schalter keine axe-Funde', async () => {
    const { wrapper } = await mountComponents()
    await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')
    await fieldByLabel(wrapper, 'Name').setValue('n'.repeat(61))
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Kennung (Slug).
  describe('Kennung', () => {
    const newButton = wrapper => wrapper.findAll('button').find(b => b.text().includes('Neue Komponente'))
    const slugField = wrapper => fieldByLabel(wrapper, 'Kennung')

    it('zeigt die Kennung in der Liste und findet sie über die Suche', async () => {
      const fetchMock = withComponents([
        { ID: 1, Name: 'API', Slug: 'api-gateway', Status: 'operational', Tags: [], Visible: true },
        { ID: 2, Name: 'Web', Slug: 'web', Status: 'operational', Tags: [], Visible: true },
      ])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      expect(wrapper.find('tbody tr .component-name__slug').text()).toBe('api-gateway')
      await wrapper.find('input[type="search"]').setValue('gateway')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    })

    it('legt neu ohne Kennung an, wenn das Feld leer bleibt (automatisch)', async () => {
      const fetchMock = withComponents([])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await newButton(wrapper).trigger('click')
      expect(slugField(wrapper).attributes('readonly')).toBeUndefined()
      expect(slugField(wrapper).attributes('maxlength')).toBe('60')
      expect(slugField(wrapper).classes()).toContain('input-mono')
      await fieldByLabel(wrapper, 'Name').setValue('Web')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(JSON.parse(lastWrite(fetchMock, 'POST')[1].body)).not.toHaveProperty('slug')
    })

    it('sendet eine eingegebene Kennung beim Anlegen', async () => {
      const fetchMock = withComponents([])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await newButton(wrapper).trigger('click')
      await fieldByLabel(wrapper, 'Name').setValue('Web')
      await slugField(wrapper).setValue('  mein-web  ')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(JSON.parse(lastWrite(fetchMock, 'POST')[1].body).slug).toBe('mein-web')
    })

    it.each(['Gross', 'mit leer', '-vorn', 'hinten-', 'dop--pelt', 'ümlaut'])('meldet die ungültige Kennung "%s" am Feld und speichert nicht', async (bad) => {
      const fetchMock = withComponents([])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await newButton(wrapper).trigger('click')
      await fieldByLabel(wrapper, 'Name').setValue('Web')
      await slugField(wrapper).setValue(bad)
      expect(slugField(wrapper).attributes('aria-invalid')).toBe('true')
      expect(wrapper.text()).toContain('Kleinbuchstaben, Ziffern und einzelne Bindestriche')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(lastWrite(fetchMock, 'POST')).toBeUndefined()
    })

    it('akzeptiert eine Kennung mit genau 60 Zeichen und lehnt 61 ab', async () => {
      const fetchMock = withComponents([])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await newButton(wrapper).trigger('click')
      await fieldByLabel(wrapper, 'Name').setValue('Web')
      await slugField(wrapper).setValue('a'.repeat(60))
      expect(slugField(wrapper).attributes('aria-invalid')).not.toBe('true')
      await slugField(wrapper).setValue('a'.repeat(61))
      expect(slugField(wrapper).attributes('aria-invalid')).toBe('true')
    })

    it.each([
      ['slug_taken', 'schon vergeben'],
      ['slug_invalid', 'Kleinbuchstaben'],
    ])('zeigt den Serverfehler %s am Feld statt als allgemeine Meldung', async (code, text) => {
      const fetchMock = withComponents([], () => jsonResponse({ error: 'x', code }, code === 'slug_taken' ? 409 : 400))
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await newButton(wrapper).trigger('click')
      await fieldByLabel(wrapper, 'Name').setValue('Web')
      await slugField(wrapper).setValue('web')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(slugField(wrapper).attributes('aria-invalid')).toBe('true')
      expect(wrapper.text()).toContain(text)
      expect(wrapper.find('.alert-error').exists()).toBe(false)
      // Eingabe räumt den Fehler wieder ab.
      await slugField(wrapper).setValue('web-2')
      expect(slugField(wrapper).attributes('aria-invalid')).not.toBe('true')
    })

    it('zeigt die Kennung beim Bearbeiten schreibgeschützt und sendet sie nicht', async () => {
      const fetchMock = withComponents([{ ID: 1, Name: 'API', Slug: 'api-gateway', Status: 'operational', Tags: [], Visible: true }])
      const { wrapper } = await mountComponents({ fetchImpl: fetchMock })
      await clickMenuItem(wrapper.find('tbody tr'), 'Bearbeiten')
      expect(slugField(wrapper).element.value).toBe('api-gateway')
      expect(slugField(wrapper).attributes('readonly')).toBeDefined()
      expect(wrapper.text()).toContain('Nach dem Anlegen nicht änderbar')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(JSON.parse(lastWrite(fetchMock, 'PUT')[1].body)).not.toHaveProperty('slug')
    })

    it('hat mit Kennungsfeld und Fehler keine axe-Funde', async () => {
      const { wrapper } = await mountComponents()
      await newButton(wrapper).trigger('click')
      await slugField(wrapper).setValue('Falsch!')
      expect(await axe(wrapper)).toHaveNoViolations()
    })
  })

  // Ohne Recht zum Ändern öffnet „Ansehen“ das Formular gesperrt, ohne Speichern.
  it('öffnet das Formular ohne Änderungsrecht gesperrt und ohne Speichern-Knopf', async () => {
    const { wrapper } = await mountComponents({ perms: 'components:read' })
    await wrapper.findAll('[role="menuitem"]').find(i => i.text() === 'Ansehen').trigger('click')
    await flushPromises()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.form-lock-hint').text()).toContain('Nur Lesezugriff')
    expect(wrapper.findAll('button[type="submit"]')).toHaveLength(0)
  })
  // Löschen meldete nichts und ließ die ältere Meldung („Komponente erstellt.“) stehen.
  it('meldet das Löschen als Erfolg und entfernt die ältere Meldung', async () => {
    stubConfirm(true)
    const { wrapper } = await mountComponents()
    wrapper.vm.showAlert('Komponente erstellt.', 'success')
    await flushPromises()

    await clickMenuItem(wrapper.find('tbody tr'), 'Löschen')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Komponente erstellt.')
    expect(wrapper.find('.alert-success').text()).toContain('Gelöscht.')
  })
})

// Komponenten archivieren (Segment Aktiv/Archiviert, Leerzustand).
describe('AdminComponents Archivieren', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  const ARCHIVED = '2026-11-01T10:00:00Z'
  const list = () => [
    { ID: 1, Name: 'Web', Status: 'operational', Tags: [], Children: [] },
    { ID: 2, Name: 'Altsystem', Status: 'operational', Tags: [], ArchivedAt: ARCHIVED, Children: [] },
    { ID: 3, Name: 'Alt-Gruppe', Status: 'group', IsGroup: true, ArchivedAt: ARCHIVED, Tags: [], Children: [
      { ID: 4, Name: 'Alt-Kind', Status: 'operational', ParentID: 3, ArchivedAt: ARCHIVED, Tags: [], Children: [] },
    ] },
  ]
  const mountArchive = async ({ data = list(), perms, postImpl } = {}) => {
    const base = makeFetchRouter()
    const fetchMock = vi.fn(async (url, options = {}) => {
      if (!options.method && /\/components$/.test(url)) return jsonResponse(data)
      if (options.method === 'POST' && /\/(archive|restore)$/.test(url)) {
        return postImpl ? postImpl(url) : jsonResponse({})
      }
      return base(url, options)
    })
    const { wrapper } = await mountComponents({ fetchImpl: fetchMock, perms })
    return { wrapper, fetchMock }
  }
  const segment = (wrapper, text) => wrapper.findAll('.segment__item').find(b => b.text().includes(text))
  const names = wrapper => wrapper.findAll('tbody tr .component-name__title').map(n => n.text())
  const postCalls = fetchMock => fetchMock.mock.calls.filter(([, o]) => o?.method === 'POST').map(([u]) => u)
  const listLoads = fetchMock => fetchMock.mock.calls.filter(([u, o]) => !o?.method && /\/components$/.test(u)).length

  it('zeigt standardmäßig nur aktive Komponenten und zählt beide Segmente', async () => {
    const { wrapper } = await mountArchive()
    expect(names(wrapper)).toEqual(['Web'])
    expect(segment(wrapper, 'Aktiv').text()).toBe('Aktiv 1')
    expect(segment(wrapper, 'Aktiv').attributes('aria-pressed')).toBe('true')
    expect(segment(wrapper, 'Archiviert').text()).toBe('Archiviert 3')
    // Untertitel zählt nur aktive Einträge
    expect(wrapper.text()).toContain('1 Komponente in 0 Gruppen')
  })

  it('zeigt im Segment „Archiviert“ nur archivierte Einträge, Gruppe samt Mitglied, ohne Schnellstatus', async () => {
    const { wrapper } = await mountArchive()
    await segment(wrapper, 'Archiviert').trigger('click')
    expect(names(wrapper)).toEqual(['Altsystem', 'Alt-Gruppe', 'Alt-Kind'])
    expect(wrapper.find('.quick-status').exists()).toBe(false)
    // Mitglied bleibt unter seiner archivierten Gruppe eingerückt, Altsystem steht oben
    const depths = wrapper.findAll('tbody tr .component-name').map(n => n.attributes('data-depth'))
    expect(depths).toEqual(['0', '0', '1'])
  })

  it('rückt ein einzeln archiviertes Mitglied einer aktiven Gruppe nicht ein', async () => {
    const data = [
      { ID: 1, Name: 'Aktive Gruppe', Status: 'group', IsGroup: true, Tags: [], Children: [
        { ID: 2, Name: 'Einzeln weg', Status: 'operational', ParentID: 1, ArchivedAt: ARCHIVED, Tags: [], Children: [] },
      ] },
    ]
    const { wrapper } = await mountArchive({ data })
    await segment(wrapper, 'Archiviert').trigger('click')
    expect(wrapper.find('tbody tr .component-name').attributes('data-depth')).toBe('0')
  })

  it('zeigt den Leerzustand „Keine archivierten Komponenten“', async () => {
    const { wrapper } = await mountArchive({ data: [list()[0]] })
    await segment(wrapper, 'Archiviert').trigger('click')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.text()).toContain('Keine archivierten Komponenten')
    expect(wrapper.text()).not.toContain('Keine Komponenten vorhanden.')
    // Gegenprobe: im aktiven Segment ohne Einträge gilt der normale Leerzustand
    const empty = await mountArchive({ data: [list()[1]] })
    expect(empty.wrapper.text()).toContain('Keine Komponenten vorhanden.')
    expect(empty.wrapper.text()).not.toContain('Keine archivierten Komponenten')
  })

  it('durchsucht nur das gewählte Segment', async () => {
    const { wrapper } = await mountArchive()
    await wrapper.find('input').setValue('Alt')
    expect(wrapper.text()).toContain('Keine Komponente gefunden.')
    await segment(wrapper, 'Archiviert').trigger('click')
    expect(names(wrapper)).toEqual(['Altsystem', 'Alt-Gruppe', 'Alt-Kind'])
  })

  it('archiviert über das Zeilenmenü per POST, meldet Erfolg und lädt neu', async () => {
    const { wrapper, fetchMock } = await mountArchive()
    const loads = listLoads(fetchMock)
    await clickMenuItem(wrapper.find('tbody tr'), 'Archivieren')
    await flushPromises()
    expect(postCalls(fetchMock)).toEqual(['/api/pages/1/components/1/archive'])
    expect(wrapper.find('.alert-success').text()).toContain('Komponente archiviert.')
    expect(listLoads(fetchMock)).toBe(loads + 1)
  })

  it('stellt im Segment „Archiviert“ per POST wieder her; „Archivieren“ fehlt dort', async () => {
    const { wrapper, fetchMock } = await mountArchive()
    await segment(wrapper, 'Archiviert').trigger('click')
    const row = wrapper.find('tbody tr')
    expect(menuItem(row, 'Archivieren')).toBeUndefined()
    await clickMenuItem(row, 'Wiederherstellen')
    await flushPromises()
    expect(postCalls(fetchMock)).toEqual(['/api/pages/1/components/2/restore'])
    expect(wrapper.find('.alert-success').text()).toContain('Komponente wiederhergestellt.')
  })

  it('bietet Archivieren/Wiederherstellen ohne components:update nicht an', async () => {
    const { wrapper } = await mountArchive({ perms: 'components:delete' })
    expect(menuItem(wrapper.find('tbody tr'), 'Archivieren')).toBeUndefined()
    await segment(wrapper, 'Archiviert').trigger('click')
    expect(menuItem(wrapper.find('tbody tr'), 'Wiederherstellen')).toBeUndefined()
  })

  it('meldet Fehler beim Archivieren und Wiederherstellen; archivierte Gruppe: eigene Meldung', async () => {
    const { wrapper } = await mountArchive({ postImpl: () => jsonResponse({ error: 'kaputt' }, 500) })
    await clickMenuItem(wrapper.find('tbody tr'), 'Archivieren')
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('Archivieren fehlgeschlagen: ')
    await segment(wrapper, 'Archiviert').trigger('click')
    await clickMenuItem(wrapper.find('tbody tr'), 'Wiederherstellen')
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('Wiederherstellen fehlgeschlagen: ')
    activeWrapper.unmount()

    const conflict = await mountArchive({ postImpl: () => jsonResponse({ error: 'x', code: 'parent_archived' }, 409) })
    await segment(conflict.wrapper, 'Archiviert').trigger('click')
    await clickMenuItem(conflict.wrapper.findAll('tbody tr')[2], 'Wiederherstellen')
    await flushPromises()
    expect(conflict.wrapper.find('.alert-error').text()).toContain('Die Gruppe ist archiviert. Stelle zuerst die Gruppe wieder her.')
  })

  it('bietet archivierte Gruppen nicht als Eltern-Gruppe an', async () => {
    const data = [
      { ID: 1, Name: 'Offene Gruppe', Status: 'group', IsGroup: true, Tags: [], Children: [] },
      { ID: 3, Name: 'Alt-Gruppe', Status: 'group', IsGroup: true, ArchivedAt: ARCHIVED, Tags: [], Children: [] },
      { ID: 5, Name: 'Komponente', Status: 'operational', Tags: [], Children: [] },
    ]
    const { wrapper } = await mountArchive({ data })
    await clickMenuItem(wrapper.findAll('tbody tr').at(-1), 'Bearbeiten')
    const options = fieldByLabel(wrapper, 'Gruppe').findAll('option').map(o => o.text())
    expect(options.some(o => o.includes('Offene Gruppe'))).toBe(true)
    expect(options.some(o => o.includes('Alt-Gruppe'))).toBe(false)
  })

  it('zeigt einem archivierten Mitglied seine (archivierte) Gruppe in der Auswahl an', async () => {
    const { wrapper } = await mountArchive()
    await segment(wrapper, 'Archiviert').trigger('click')
    await clickMenuItem(wrapper.findAll('tbody tr')[2], 'Bearbeiten')
    const select = fieldByLabel(wrapper, 'Gruppe')
    expect(select.element.value).toBe('3')
    expect(select.findAll('option').some(o => o.text().includes('Alt-Gruppe'))).toBe(true)
  })

  it('hat im Segment „Archiviert“ keine axe-Funde', async () => {
    const { wrapper } = await mountArchive()
    await segment(wrapper, 'Archiviert').trigger('click')
    expect(await axe(wrapper.element)).toHaveNoViolations()
  })
})
