import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { withArchivedComponent, componentTreeFixture, expectedTreeOptionLabels, multiSelectOptionLabels } from '../../test-support/componentTree'
import AdminIncidentTemplates from './AdminIncidentTemplates.vue'
import { axe } from '../../test-support/axe.js'
import { fieldByLabel, hasLabel } from '../../test-support/fields.js'
import { clickMenuItem } from '../../test-support/rowMenu.js'
import { stubConfirm } from '../../test-support/confirm'

function templatesFixture() {
  return [
    { ID: 1, Name: 'DB-Wartung', Kind: 'maintenance', Title: 'Datenbank-Wartung EU-West', Message: 'Details', ComponentIDs: '10' },
  ]
}

function componentsFixture() {
  // Baum mit Unterkomponente statt nur oberster Ebene.
  return componentTreeFixture()
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ loadOk = true, writeOk = true } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/incident-templates$/.test(url)) {
      return loadOk ? jsonResponse(templatesFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'GET' && /\/components$/.test(url)) return jsonResponse(componentsFixture())
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

async function mountTemplates({ fetchImpl, perms = 'incident_templates:create,incident_templates:update,incident_templates:delete', tenantId = '1' } = {}) {
  setAuthAsPageRole('jwt-1', { permissions: perms })
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminIncidentTemplates, {
    global: { plugins: [i18n], stubs: onyxFormStubs },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminIncidentTemplates', () => {
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

  // vorher bot die Auswahl nur die oberste Ebene an, und der
  // Name einer Unterkomponente fehlte in der Tabelle.
  it('bietet Unterkomponenten eingerückt an und zeigt ihren Namen in der Tabelle', async () => {
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) => (!options.method && /\/incident-templates$/.test(url))
      ? { ok: true, status: 200, json: async () => [{ ID: 2, Name: 'GW', Kind: 'incident', Title: 'T', Message: 'M', ComponentIDs: '12' }] }
      : base(url, options))
    const { wrapper } = await mountTemplates({ fetchImpl })

    expect(wrapper.find('tbody tr').text()).toContain('API Gateway (US)')
    await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
    expect(multiSelectOptionLabels(wrapper)).toEqual(expectedTreeOptionLabels)
  })

  // eine inzwischen archivierte Komponente einer bestehenden Vorlage bleibt in der Auswahl sichtbar.
  it('zeigt beim Bearbeiten eine archivierte, gewählte Komponente weiter in der Auswahl', async () => {
    const base = makeFetchRouter()
    const fetchImpl = withArchivedComponent(vi.fn(async (url, options = {}) => (!options.method && /\/incident-templates$/.test(url))
      ? { ok: true, status: 200, json: async () => [{ ID: 2, Name: 'Alt', Kind: 'incident', Title: 'T', Message: 'M', ComponentIDs: '13' }] }
      : base(url, options)))
    const { wrapper } = await mountTemplates({ fetchImpl })
    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(multiSelectOptionLabels(wrapper)).toContain('Altsystem')
    await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
    expect(multiSelectOptionLabels(wrapper)).not.toContain('Altsystem')
  })

  it('lädt Vorlagen + Komponenten und zeigt Typ/Titel/Komponentennamen', async () => {
    const { wrapper } = await mountTemplates()

    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('DB-Wartung')
    expect(row.text()).toContain('Wartungsfenster')
    expect(row.text()).toContain('Datenbank-Wartung EU-West')
    expect(row.text()).toContain('API')
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountTemplates({ fetchImpl: makeFetchRouter({ loadOk: false }) })

    expect(wrapper.find('.alert-error').text()).toContain('kaputt')
  })

  // Nachrichtenfeld der Vorlage ist der Editor.
  it('Nachrichtenfeld der Vorlage ist der RichTextEditor', async () => {
    const { wrapper } = await mountTemplates({ fetchImpl: makeFetchRouter() })
    await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
    expect(fieldByLabel(wrapper, 'Nachricht / Details').element.closest('.rte')).not.toBeNull()
    expect(wrapper.find('.rte [role="toolbar"]').exists()).toBe(true)
  })

  it('legt eine neue Vorlage per POST an und zeigt eine Erfolgsmeldung', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountTemplates({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
    await fieldByLabel(wrapper, 'Interner Vorlagenname').setValue('Neue Vorlage')
    await fieldByLabel(wrapper, 'Titel').setValue('Titel X')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Nachrichtentext')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const [url, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST')
    expect(url).toContain('/incident-templates')
    expect(JSON.parse(options.body)).toMatchObject({
      Name: 'Neue Vorlage', Kind: 'incident', Title: 'Titel X', Message: 'Nachrichtentext',
    })
    expect(wrapper.find('.alert-success').text()).toContain('Vorlage erstellt.')
  })

  it('lädt eine bestehende Vorlage zum Bearbeiten inkl. gesplitteter ComponentIDs', async () => {
    const { wrapper } = await mountTemplates()

    await clickMenuItem(wrapper, 'Bearbeiten')

    expect(fieldByLabel(wrapper, 'Interner Vorlagenname').element.value).toBe('DB-Wartung')
  })

  it('löscht eine Vorlage nach Bestätigung per DELETE', async () => {
    const confirm = stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountTemplates({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()

    expect(confirm).toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/incident-templates/1'), expect.objectContaining({ method: 'DELETE' }))
  })

  it('versteckt Neu-Button und Aktionen-Spalte ohne incident_templates:create/update/delete-Recht', async () => {
    const { wrapper } = await mountTemplates({ perms: 'incident_templates:read' })

    expect(wrapper.find('.page-head button').exists()).toBe(false)
    // Leser dürfen ansehen, aber nicht ändern oder löschen.
    expect(wrapper.findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Ansehen'])
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    const { wrapper } = await mountTemplates()
    expect(await axe(wrapper)).toHaveNoViolations()

    await clickMenuItem(wrapper, 'Bearbeiten')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('zeigt Liste und Formular nebeneinander und den Leerzustand als EmptyState', async () => {
    const { wrapper } = await mountTemplates()
    expect(wrapper.find('.split').classes()).not.toContain('split--open')
    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('table.data-table').exists()).toBe(true)
    expect(wrapper.find('aside.form-panel').attributes('aria-label')).toContain('bearbeiten')
    expect(await axe(wrapper)).toHaveNoViolations()

    const base = makeFetchRouter()
    const empty = vi.fn(async (url, options = {}) => (!options.method && /\/incident-templates$/.test(url))
      ? { ok: true, status: 200, json: async () => [] }
      : base(url, options))
    const { wrapper: none } = await mountTemplates({ fetchImpl: empty })
    expect(none.find('.empty-state__title').exists()).toBe(true)
  })

  // Stufe 4c: Typ-/Status-Auswahl und Komponenten im Formular.
  it('übernimmt Typ, Standardstatus und Komponenten aus dem Formular; Wartung blendet den Status aus', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountTemplates({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
    await fieldByLabel(wrapper, 'Interner Vorlagenname').setValue('V')
    await fieldByLabel(wrapper, 'Titel').setValue('T')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('M')
    await fieldByLabel(wrapper, 'Standard-Status').setValue('resolved')
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    await fieldByLabel(wrapper, 'Typ').setValue('incident')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(JSON.parse(options.body)).toMatchObject({ Kind: 'incident', DefaultStatus: 'resolved', Message: 'M' })
    expect(JSON.parse(options.body).ComponentIDs).not.toBe('')

    await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
    await fieldByLabel(wrapper, 'Typ').setValue('maintenance')
    expect(hasLabel(wrapper, 'Standard-Status')).toBe(false)
  })
  describe('Richtung C', () => {
    const templates = () => [
      { ID: 1, Name: 'DB-Wartung', Kind: 'maintenance', Title: 'Datenbank-Wartung', Message: 'Details', ComponentIDs: '10' },
      { ID: 2, Name: 'API-Ausfall', Kind: 'incident', Title: 'API gestört', Message: 'Wir prüfen', ComponentIDs: '' },
    ]
    const withTemplates = () => {
      const base = makeFetchRouter()
      return vi.fn(async (url, options = {}) => (!options.method && /\/incident-templates$/.test(url))
        ? { ok: true, status: 200, json: async () => templates() }
        : base(url, options))
    }

    it('zeigt Brotkrumen, Untertitel mit Zählung und das Zeilenmenü statt Aktionsknöpfen', async () => {
      useTenant().setMyPages([{ ID: 1, Name: 'Meine Seite' }])
      const { wrapper } = await mountTemplates({ fetchImpl: withTemplates() })
      expect(wrapper.find('.page-head__crumbs').text()).toContain('Meine Seite')
      expect(wrapper.find('.page-head__subtitle').text()).toContain('2 Vorlagen')
      expect(wrapper.find('thead').text()).not.toContain('ID')
      expect(wrapper.find('tbody .action-btn').exists()).toBe(false)
      expect(wrapper.find('.row-menu__button').attributes('aria-label')).toBe('Aktionen für „DB-Wartung“')
    })

    it('sucht und filtert nach Typ', async () => {
      const { wrapper } = await mountTemplates({ fetchImpl: withTemplates() })
      const seg = (text) => wrapper.findAll('.segment__item').find(b => b.text() === text)
      await seg('Vorfall').trigger('click')
      expect(wrapper.findAll('tbody tr').map(r => r.text())).toEqual([expect.stringContaining('API-Ausfall')])
      await seg('Alle').trigger('click')
      await wrapper.find('input[type="search"]').setValue('datenbank')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      await wrapper.find('input[type="search"]').setValue('gibtesnicht')
      expect(wrapper.find('.empty-state__title').text()).toBe('Keine Vorlage gefunden.')
    })

    it('Formulartitel nennt die Vorlage, Pflichtfelder zeigen Fehler am Feld ohne Request', async () => {
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountTemplates({ fetchImpl: fetchMock })
      await clickMenuItem(wrapper, 'Bearbeiten')
      expect(wrapper.find('.form-panel h3').text()).toBe('Vorlage „DB-Wartung“ bearbeiten')
      await wrapper.find('.form-panel__close').trigger('click')

      await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(wrapper.findAll('.form-field__message--error')).toHaveLength(3)
      expect(fieldByLabel(wrapper, 'Interner Vorlagenname').attributes('aria-invalid')).toBe('true')
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
    })

    // Regression: der Standard-Status stand fest auf Englisch im Quelltext
    // („Investigating“ ...), obwohl die Vorfall-Ansichten ihn übersetzen.
    it('bietet den Standard-Status in der UI-Sprache an', async () => {
      const { wrapper } = await mountTemplates()
      await wrapper.findAll('button').find(b => b.text().includes('Neue Vorlage')).trigger('click')
      expect(fieldByLabel(wrapper, 'Standard-Status').findAll('option').map(o => o.text())).toEqual(['Untersuchung', 'Identifiziert', 'Beobachtung', 'Behoben'])
    })

    it('sucht auch in Vorlagen ohne Titel und Nachricht', async () => {
      const base = makeFetchRouter()
      const fetchMock = vi.fn(async (url, options = {}) => (!options.method && /\/incident-templates$/.test(url))
        ? { ok: true, status: 200, json: async () => [{ ID: 3, Name: 'Nur Name', Kind: 'incident', Title: null, Message: null, ComponentIDs: '' }] }
        : base(url, options))
      const { wrapper } = await mountTemplates({ fetchImpl: fetchMock })
      await wrapper.find('input[type="search"]').setValue('name')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      await wrapper.find('input[type="search"]').setValue('null')
      expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    })
  })
})
