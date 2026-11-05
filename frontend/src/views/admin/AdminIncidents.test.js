import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { withArchivedComponent, componentTreeFixture, expectedTreeOptionLabels, multiSelectOptionLabels } from '../../test-support/componentTree'
import { toLocalInputValue } from '../../utils/datetime'
import AdminIncidents from './AdminIncidents.vue'
import { axe } from '../../test-support/axe.js'
import { expectNoInlineStyles } from '../../test-support/noInlineStyles.js'
import { fieldByLabel } from '../../test-support/fieldByLabel.js'
import { clickMenuItem, menuItem } from '../../test-support/rowMenu.js'
import { stubConfirm } from '../../test-support/confirm'

function incidentsFixture() {
  return [
    {
      ID: 1, Title: 'API down', Message: 'API antwortet nicht', Status: 'investigating', Severity: 'partial_outage',
      Components: [{ ID: 10, Name: 'API' }], Tags: [{ ID: 5, Name: 'DE' }], CreatedAt: '2026-07-01T10:00:00Z',
    },
  ]
}
function componentsFixture() {
  // Baum mit Unterkomponente statt nur oberster Ebene.
  return componentTreeFixture()
}
function tagsFixture() {
  return [{ ID: 5, Name: 'DE' }, { ID: 6, Name: 'US' }]
}
function pageTagsFixture() {
  return [
    { ID: 5, Name: 'global-region', StatusPageID: null },
    { ID: 7, Name: 'eigen-team', StatusPageID: 1 },
  ]
}
function templatesFixture() {
  return [{
    ID: 1, Name: 'Standard-Ausfall', Kind: 'incident', Title: 'X ist gestört',
    Message: 'Wir untersuchen den Vorfall.', DefaultStatus: 'monitoring', ComponentIDs: '10,11',
  }]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// Routet den gemockten fetch anhand von Methode + URL auf die jeweilige
// Fixture, damit ein einziger Mock für Promise.all(incidents, components)
// plus die separaten Tags-/Templates-Requests reicht.
function makeFetchRouter({
  incidentsOk = true, componentsOk = true, tagsOk = true, templatesOk = true,
  writeOk = true, writeStatus, writeResponse, incidentsFixtureOverride, pageTags = pageTagsFixture(),
} = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/incident-templates$/.test(url)) {
      return templatesOk ? jsonResponse(templatesFixture()) : jsonResponse({}, 403)
    }
    if (method === 'GET' && /\/incidents\?limit=200(&offset=\d+)?$/.test(url)) {
      return incidentsOk ? jsonResponse(incidentsFixtureOverride ?? incidentsFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'GET' && /\/components$/.test(url)) {
      return componentsOk ? jsonResponse(componentsFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    // Seiten-Endpunkt für Seiten-Rollen (globale + seiteneigene Tags).
    if (method === 'GET' && /\/api\/pages\/\d+\/tags$/.test(url)) return jsonResponse(pageTags)
    if (method === 'GET' && /\/api\/tags/.test(url)) {
      return tagsOk ? jsonResponse(tagsFixture()) : jsonResponse({}, 403)
    }
    if (method === 'POST' && /\/incidents$/.test(url)) {
      return writeOk ? jsonResponse(writeResponse || { ID: 99 }) : jsonResponse({ error: 'Speichern kaputt' }, writeStatus || 500)
    }
    if (method === 'PUT' && /\/incidents\/\d+$/.test(url)) {
      return writeOk ? jsonResponse(writeResponse || { ID: 1 }) : jsonResponse({ error: 'Speichern kaputt' }, writeStatus || 500)
    }
    if (method === 'DELETE' && /\/incidents\/\d+$/.test(url)) {
      return writeOk ? { ok: true, status: 204 } : jsonResponse({ error: 'Löschen kaputt' }, writeStatus || 500)
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/admin/incidents/:pageId/:id', component: { template: '<div/>' } }],
  })
}

let activeWrapper = null

async function mountAdminIncidents({ fetchImpl, tenantId = '1' } = {}) {
  const router = makeRouter()
  router.push('/admin/incidents/1/0')
  await router.isReady()

  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminIncidents, {
    global: {
      plugins: [i18n, router],
      stubs: onyxFormStubs,
    },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper, router }
}

// Rechte je Tab statt incidents:write.
const INCIDENT_PERMS = 'incidents:create,incidents:update,incidents:delete,incident_templates:read'

describe('AdminIncidents', () => {
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

  // vorher bot die Auswahl nur die oberste Ebene an.
  it('bietet in der Komponenten-Auswahl auch Unterkomponenten eingerückt an', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()
    await wrapper.find('.page-head button').trigger('click')

    expect(multiSelectOptionLabels(wrapper)).toEqual(expectedTreeOptionLabels)
  })

  // eine inzwischen archivierte Komponente eines bestehenden Vorfalls bleibt in der Auswahl sichtbar.
  it('zeigt beim Bearbeiten eine archivierte, gewählte Komponente weiter in der Auswahl', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const items = [{ ...incidentsFixture()[0], Components: [{ ID: 13, Name: 'Altsystem' }] }]
    const { wrapper } = await mountAdminIncidents({ fetchImpl: withArchivedComponent(makeFetchRouter({ incidentsFixtureOverride: items })) })
    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(multiSelectOptionLabels(wrapper)).toContain('Altsystem')
    await wrapper.find('.page-head button').trigger('click')
    expect(multiSelectOptionLabels(wrapper)).not.toContain('Altsystem')
  })

  // Das Nachrichtenfeld ist der Editor (Werkzeugleiste, Vorschau, Zähler), das Label führt weiter zum Feld.
  it('Nachrichtenfeld ist der RichTextEditor und sendet den Markdown-Text', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()
    await wrapper.find('.page-head button').trigger('click')

    const field = fieldByLabel(wrapper, 'Nachricht / Details')
    expect(field.element.closest('.rte')).not.toBeNull()
    expect(wrapper.find('.rte [role="toolbar"]').exists()).toBe(true)
    await field.setValue('**fett**')
    expect(wrapper.find('.rte__counter').text()).toBe('8 / 5.000 Zeichen')
    await wrapper.find('.rte__btn--preview').trigger('click')
    expect(wrapper.find('.rte__preview strong').text()).toBe('fett')
  })

  describe('Weitere Einträge laden', () => {
    const many = (from, n) => Array.from({ length: n }, (_, i) => ({
      ID: from + i, Title: `Vorfall ${from + i}`, Message: 'm', Status: 'resolved', Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z',
    }))
    const routed = (first, second) => {
      const base = makeFetchRouter()
      return vi.fn(async (url, options) => {
        if (url === '/api/pages/1/incidents?limit=200') return jsonResponse(first)
        if (url === '/api/pages/1/incidents?limit=200&offset=200') return jsonResponse(second)
        return base(url, options)
      })
    }

    it('zeigt keinen Knopf, solange die Seite nicht voll ist', async () => {
      setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: INCIDENT_PERMS })
      const { wrapper } = await mountAdminIncidents({ fetchImpl: routed(many(1, 199), []) })
      expect(wrapper.find('[data-test="load-more"]').exists()).toBe(false)
    })

    it('holt bei voller Seite die nächsten 200 und blendet den Knopf danach aus', async () => {
      setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: INCIDENT_PERMS })
      const fetchMock = routed(many(1, 200), many(201, 5))
      const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
      expect(wrapper.findAll('tbody tr')).toHaveLength(200)

      await wrapper.find('[data-test="load-more"]').trigger('click')
      await flushPromises()

      expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents?limit=200&offset=200', expect.anything())
      expect(wrapper.findAll('tbody tr')).toHaveLength(205)
      expect(wrapper.find('[data-test="load-more"]').exists()).toBe(false)
    })

    it('behält den Knopf, wenn auch die nachgeladene Seite voll ist, und sperrt ihn während des Ladens', async () => {
      setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: INCIDENT_PERMS })
      let release
      const pending = new Promise(resolve => { release = resolve })
      const base = routed(many(1, 200), [])
      const fetchMock = vi.fn(async (url, options) => {
        if (url === '/api/pages/1/incidents?limit=200&offset=200') {
          await pending
          return jsonResponse(many(201, 200))
        }
        return base(url, options)
      })
      const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })

      await wrapper.find('[data-test="load-more"]').trigger('click')
      expect(wrapper.find('[data-test="load-more"]').attributes('disabled')).toBeDefined()
      release()
      await flushPromises()

      expect(wrapper.findAll('tbody tr')).toHaveLength(400)
      expect(wrapper.find('[data-test="load-more"]').exists()).toBe(true) // genau 200 geholt: es kann mehr geben
      expect(wrapper.find('[data-test="load-more"]').attributes('disabled')).toBeUndefined()
    })

    it('meldet einen Fehler beim Nachladen und behält die Liste', async () => {
      setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: INCIDENT_PERMS })
      const base = routed(many(1, 200), [])
      const fetchMock = vi.fn(async (url, options) => {
        if (url.endsWith('offset=200')) return jsonResponse({ error: 'kaputt' }, 500)
        return base(url, options)
      })
      const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })

      await wrapper.find('[data-test="load-more"]').trigger('click')
      await flushPromises()

      expect(wrapper.findAll('tbody tr')).toHaveLength(200)
      expect(wrapper.find('[data-test="load-more"]').exists()).toBe(true)
      expect(wrapper.find('.alert-error').exists()).toBe(true)
    })
  })

  it('lädt Incidents, Components und Tags, sobald eine Statusseite aktiv ist, und rendert die Tabelle', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })

    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents?limit=200', expect.anything())
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/components', expect.anything())
    expect(fetchMock).toHaveBeenCalledWith('/api/tags?status_page_id=1', expect.anything())

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('API down')
    expect(rows[0].text()).toContain('API')
  })

  it('verlinkt je Vorfall die Übersetzungsseite', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()

    const link = menuItem(wrapper, 'Übersetzungen')
    expect(link.attributes('href')).toBe('/admin/incidents/1/translations')
  })

  it('bietet im Zeilenmenü „Update posten“ als Link zum Update-Formular (nur mit Anlegerecht)', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()
    expect(menuItem(wrapper, 'Update posten').attributes('href')).toBe('/admin/incidents/1/1#update')

    setAuthAsPageRole('jwt-1', { permissions: 'incidents:read,incidents:update' })
    const { wrapper: ohneAnlegen } = await mountAdminIncidents()
    expect(ohneAnlegen.findAll('[role="menuitem"]').map(i => i.text())).not.toContain('Update posten')
  })

  it('zeigt eine Fehlermeldung, wenn Incidents/Components nicht geladen werden können', async () => {
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsOk: false }) })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('Fehler beim Laden')
  })

  it('lädt Tags fehlertolerant, ohne die restliche Ansicht zu blockieren', async () => {
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ tagsOk: false }) })

    expect(wrapper.find('.alert-error').exists()).toBe(false)
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })

  it('lädt Vorlagen nur mit incidents:write-Berechtigung', async () => {
    setAuthAsPageRole('jwt-1', { permissions: '' })
    const fetchMock = makeFetchRouter()
    await mountAdminIncidents({ fetchImpl: fetchMock })

    expect(fetchMock).not.toHaveBeenCalledWith(expect.stringContaining('incident-templates'), expect.anything())
  })

  it('zeigt den "+ Neuer Vorfall"-Button nur mit incidents:write-Berechtigung', async () => {
    const { wrapper: withoutPerm } = await mountAdminIncidents()
    expect(withoutPerm.text()).not.toContain('Neuer Vorfall')

    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper: withPerm } = await mountAdminIncidents()
    expect(withPerm.text()).toContain('Neuer Vorfall')
  })

  it('öffnet ein leeres Formular mit Status "investigating" über "+ Neuer Vorfall"', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()

    await wrapper.find('button').trigger('click')

    expect(wrapper.find('.edit-form').exists()).toBe(true)
    expect(wrapper.find('.edit-status-hint').exists()).toBe(false) // Status-Select statt Hint bei neuem Incident
  })

  it('übernimmt Titel/Message/Status/Komponenten aus einer gewählten Vorlage', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()
    await wrapper.find('.page-head button').trigger('click')

    const templateSelect = fieldByLabel(wrapper, 'Vorlage verwenden')
    await templateSelect.setValue('1')

    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('X ist gestört')
    expect(fieldByLabel(wrapper, 'Nachricht / Details').element.value).toBe('Wir untersuchen den Vorfall.')
  })

  it('befüllt das Formular beim Bearbeiten aus dem bestehenden Incident', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()

    await clickMenuItem(wrapper, 'Bearbeiten')

    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('API down')
    // Beim Bearbeiten wird der Status-Select durch den Verlaufs-Hinweis ersetzt
    expect(wrapper.find('.edit-status-hint').exists()).toBe(true)
    expect(wrapper.find('.edit-status-hint a').attributes('href')).toBe('/admin/incidents/1/1')
  })

  it('legt einen neuen Vorfall per POST an und lädt die Liste neu', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })

    await wrapper.find('.page-head button').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Neuer Vorfall')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Details...')
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)

    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents', expect.objectContaining({ method: 'POST' }))
    expect(wrapper.find('.alert-success').text()).toContain('Vorfall erstellt.')
    expect(wrapper.find('.edit-form').exists()).toBe(false)
  })

  it('sendet bei einem neuen Vorfall ohne Tags leere Tags und Status investigating', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })

    await wrapper.find('.page-head button').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Leer')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Details')
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    const post = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(JSON.parse(post[1].body)).toEqual({
      Status: 'investigating', Severity: 'degraded_performance', ComponentIDs: [10], TagIDs: [], StartedAt: null, ResolvedAt: null,
      Title: 'Leer', Message: 'Details',
    })
  })

  it('sendet Beginn/Ende als ISO-String, leer als null', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    const postBody = () => JSON.parse(fetchMock.mock.calls.filter(([, o]) => o?.method === 'POST').at(-1)[1].body)
    const fill = async () => {
      await wrapper.find('.page-head button').trigger('click')
      await fieldByLabel(wrapper, 'Titel').setValue('Alt')
      await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Details')
      await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    }

    await fill()
    await wrapper.find('input[data-test="started-at"]').setValue('2026-10-02T08:00')
    await wrapper.find('input[data-test="resolved-at"]').setValue('2026-10-02T09:30')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(postBody().StartedAt).toBe(new Date('2026-10-02T08:00').toISOString())
    expect(postBody().ResolvedAt).toBe(new Date('2026-10-02T09:30').toISOString())

    await fill()
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(postBody().StartedAt).toBeNull()
    expect(postBody().ResolvedAt).toBeNull()
  })

  it('befüllt Beginn/Ende beim Bearbeiten aus dem Vorfall', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const items = [{ ...incidentsFixture()[0], StartedAt: '2026-10-02T08:00:00Z', ResolvedAt: null }]
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: items }) })
    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(wrapper.find('input[data-test="started-at"]').element.value).toBe(toLocalInputValue('2026-10-02T08:00:00Z'))
    expect(wrapper.find('input[data-test="resolved-at"]').element.value).toBe('')
  })

  it('sendet ein geleertes Beginn-Feld beim Bearbeiten als null', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const items = [{ ...incidentsFixture()[0], StartedAt: '2026-10-02T08:00:00Z', ResolvedAt: null }]
    const fetchMock = makeFetchRouter({ incidentsFixtureOverride: items })
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper, 'Bearbeiten')
    await wrapper.find('input[data-test="started-at"]').setValue('')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    const put = fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT').at(-1)[1]
    expect(JSON.parse(put.body).StartedAt).toBeNull()
  })

  it('übernimmt Beginn und Ende beim Bearbeiten unverändert in den PUT', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const items = [{ ...incidentsFixture()[0], StartedAt: '2026-10-02T08:00:00Z', ResolvedAt: '2026-10-02T09:30:00Z' }]
    const fetchMock = makeFetchRouter({ incidentsFixtureOverride: items })
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(wrapper.find('input[data-test="resolved-at"]').element.value).toBe(toLocalInputValue('2026-10-02T09:30:00Z'))
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    const put = JSON.parse(fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT').at(-1)[1].body)
    expect(put.StartedAt).toBe(new Date('2026-10-02T08:00:00Z').toISOString())
    expect(put.ResolvedAt).toBe(new Date('2026-10-02T09:30:00Z').toISOString())
  })

  it('bearbeitet einen Vorfall ohne Komponenten, Tags und Komponenten-Auswahl', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const items = [{ ...incidentsFixture()[0], Components: undefined, Tags: undefined }]
    const fetchMock = makeFetchRouter({ incidentsFixtureOverride: items })
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(wrapper.vm.formData.ComponentIDs).toEqual([])
    expect(wrapper.vm.formData.TagIDs).toEqual([])
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    const put = JSON.parse(fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT').at(-1)[1].body)
    expect(put.ComponentIDs).toEqual([10])
  })

  // Schweregrad im Formular, Default Beeinträchtigt, beim Bearbeiten änderbar.
  it('sendet den gewählten Schweregrad beim Anlegen (Default degraded_performance)', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    await wrapper.find('.page-head button').trigger('click')
    const select = wrapper.find('select[data-test="severity"]')
    expect(select.element.value).toBe('degraded_performance')
    expect(select.findAll('option').map(o => o.text())).toEqual(['Beeinträchtigt', 'Teilausfall', 'Großstörung'])
    await select.setValue('major_outage')
    await fieldByLabel(wrapper, 'Titel').setValue('X')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Y')
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    const post = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(JSON.parse(post[1].body).Severity).toBe('major_outage')
  })

  it('zeigt beim Bearbeiten den gespeicherten Schweregrad und sendet die Änderung', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    await clickMenuItem(wrapper, 'Bearbeiten')
    const select = wrapper.find('select[data-test="severity"]')
    expect(select.element.value).toBe('partial_outage')
    await select.setValue('major_outage')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    const put = JSON.parse(fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT').at(-1)[1].body)
    expect(put.Severity).toBe('major_outage')
  })

  it('speichert eine Bearbeitung per PUT an die Incident-ID', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper, 'Bearbeiten')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents/1', expect.objectContaining({ method: 'PUT' }))
    expect(wrapper.find('.alert-success').text()).toContain('Änderungen gespeichert.')
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern fehlschlägt', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await clickMenuItem(wrapper, 'Bearbeiten')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Speichern fehlgeschlagen')
    expect(wrapper.find('.edit-form').exists()).toBe(true)
  })

  it('löscht einen Vorfall nach Bestätigung per DELETE und lädt neu', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const confirm = stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()

    expect(confirm).toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/incidents/1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('löscht nicht, wenn die Bestätigung abgelehnt wird', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    stubConfirm(false)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    fetchMock.mockClear()

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ method: 'DELETE' }))
  })

  it('zeigt eine Fehlermeldung, wenn das Löschen fehlschlägt', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    stubConfirm(true)
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Löschen fehlgeschlagen')
  })

  it('blendet die Aktionen-Spalte ohne incidents:write-Berechtigung aus', async () => {
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter() })
    // Ohne Recht nur Ansehen und Übersetzungen, kein Bearbeiten/Löschen.
    const entries = (w) => [...new Set(w.findAll('[role="menuitem"]').map(i => i.text()))]
    expect(entries(wrapper)).toEqual(['Ansehen', 'Übersetzungen'])

    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper: withPerm } = await mountAdminIncidents({ fetchImpl: makeFetchRouter() })
    expect(entries(withPerm)).toEqual(['Bearbeiten', 'Update posten', 'Übersetzungen', 'Löschen'])
  })

  it('zeigt einen Leerzustand, wenn keine Vorfälle vorhanden sind', async () => {
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: [] }) })

    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state__title').text()).toBe('Keine Vorfälle vorhanden.')
  })

  it('zeigt bei 403 die Berechtigungsmeldung statt Tabelle und Formular', async () => {
    const { wrapper } = await mountAdminIncidents({ fetchImpl: vi.fn(async () => jsonResponse({}, 403)) })

    expect(wrapper.text()).toContain('Keine Berechtigung für diesen Bereich')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.edit-card').exists()).toBe(false)
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('stürzt bei einer null-Antwort nicht ab und zeigt eine leere Liste', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents({ fetchImpl: vi.fn(async () => jsonResponse(null)) })

    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.find('.alert-error').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Keine Berechtigung')

    await wrapper.find('button').trigger('click')
    expect(wrapper.find('.edit-form').exists()).toBe(true)
    expect(wrapper.findAll('.onyx-select-stub-option')).toHaveLength(0)
  })

  it('zeigt ohne aktive Statusseite weder Berechtigungsmeldung noch Fehler', async () => {
    const { wrapper } = await mountAdminIncidents({ fetchImpl: vi.fn(), tenantId: null })

    expect(wrapper.text()).not.toContain('Keine Berechtigung')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('zeigt bei 500 weiterhin den Lade-Alert und nicht die Berechtigungsmeldung', async () => {
    const { wrapper } = await mountAdminIncidents({ fetchImpl: vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500)) })

    expect(wrapper.find('.alert-error').text()).toContain('Fehler beim Laden')
    expect(wrapper.text()).not.toContain('Keine Berechtigung')
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()
    expect(await axe(wrapper)).toHaveNoViolations()

    await clickMenuItem(wrapper, 'Bearbeiten')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // /api/tags ist global-admin-only. Seiten-Rollen laden die
  // Tags über /api/pages/:id/tags und sehen nur seiteneigene Tags; gibt es keine,
  // fehlt das Feld ganz (statt „Keine Tags vorhanden. Unter ‚Tags‘ anlegen.“).
  it('zeigt Editoren nur seiteneigene Tags, blendet das Feld ohne sie aus und zeigt es Global Admins', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const editorFetch = makeFetchRouter()
    const { wrapper: editor } = await mountAdminIncidents({ fetchImpl: editorFetch })
    await editor.find('button').trigger('click')
    await flushPromises()
    expect(editor.find('.edit-form').exists()).toBe(true)
    expect(editor.findAll('.edit-form .form-field__label').map(l => l.text())).toContain('Tags (Reichweite)')
    expect(editor.find('.edit-form .tag-chips__add').text()).toContain('eigen-team')
    expect(editor.find('.edit-form .tag-chips__add').text()).not.toContain('global-region')
    expect(editorFetch.mock.calls.some(([u]) => u.includes('/api/tags'))).toBe(false)
    editor.unmount()

    const noOwnTags = makeFetchRouter({ pageTags: [{ ID: 5, Name: 'global-region', StatusPageID: null }] })
    const { wrapper: bare } = await mountAdminIncidents({ fetchImpl: noOwnTags })
    await bare.find('button').trigger('click')
    await flushPromises()
    expect(bare.findAll('.edit-form .form-field__label').map(l => l.text())).not.toContain('Tags (Reichweite)')
    expect(bare.text()).not.toContain('Keine Tags vorhanden')
    bare.unmount()

    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
    const { wrapper: admin } = await mountAdminIncidents()
    await admin.find('button').trigger('click')
    await flushPromises()
    expect(admin.findAll('.edit-form .form-field__label').map(l => l.text())).toContain('Tags (Reichweite)')
  })
  it('Seitentitel ist eine h2 (PageHeader level 2, die Topbar trägt die h1), Karten- und Formulartitel eine h3', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()

    expect(wrapper.findAll('h2.page-head__title').map(h => h.text())).toEqual(['Vorfälle'])
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('.page-header').exists()).toBe(false)

    await wrapper.find('.page-head button').trigger('click')
    expect(wrapper.findAll('h2.page-head__title')).toHaveLength(1)
    expect(wrapper.find('.form-panel h3').text()).toBe('Neuen Vorfall anlegen')
    expect(wrapper.find('.comp-card h3').text()).toBe('Alle Vorfälle')
  })

  // Richtung C: Seitenkopf, Kartenkopf mit Suche, Zeilenmenü, relative Zeit.
  describe('Richtung C', () => {
    const manyIncidents = () => [
      { ID: 1, Title: 'API down', Message: 'API antwortet nicht', Status: 'investigating', Components: [{ ID: 10, Name: 'API' }], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' },
      { ID: 2, Title: 'Login langsam', Message: 'Dauert lange', Status: 'resolved', Components: [{ ID: 11, Name: 'Auth' }], Tags: [], CreatedAt: '2026-07-02T10:00:00Z' },
    ]

    it('zeigt Brotkrumen mit Seitenname, Untertitel mit Zählung und die Spalte „Erstellt“ relativ', async () => {
      useTenant().setMyPages([{ ID: 1, Name: 'Meine Seite' }])
      const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: manyIncidents() }) })

      expect(wrapper.find('.page-head__crumbs').text()).toContain('Meine Seite')
      expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe('Vorfälle')
      expect(wrapper.find('.page-head__subtitle').text()).toBe('2 Vorfälle · 1 offen')
      const time = wrapper.find('tbody tr time')
      expect(time.attributes('datetime')).toBe('2026-07-01T10:00:00Z')
      expect(time.text()).toMatch(/vor .*(Monat|Jahr|Tag)/)
      expect(wrapper.find('thead').text()).not.toContain('ID')
    })

    it('hat keine Aktionsknöpfe mehr in der Zeile, nur das Zeilenmenü', async () => {
      setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
      const { wrapper } = await mountAdminIncidents()
      expect(wrapper.find('tbody .action-btn').exists()).toBe(false)
      expect(wrapper.find('tbody .row-menu__button').attributes('aria-label')).toBe('Aktionen für „API down“')
      expect(wrapper.findAll('tbody [role="menuitem"]').map(i => i.text())).toEqual(['Bearbeiten', 'Update posten', 'Übersetzungen', 'Löschen'])
    })

    it('ohne Schreibrecht enthält das Zeilenmenü nur Ansehen und Übersetzungen', async () => {
      const { wrapper } = await mountAdminIncidents()
      expect(wrapper.findAll('[role="menuitem"]').every(i => ['Ansehen', 'Übersetzungen'].includes(i.text()))).toBe(true)
    })

    it('sucht in Titel, Nachricht und Komponente und meldet leere Treffer als Leerzustand', async () => {
      const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: manyIncidents() }) })
      const search = wrapper.find('input[type="search"]')
      await search.setValue('auth')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      expect(wrapper.find('tbody').text()).toContain('Login langsam')
      expect(wrapper.find('[role="status"].sr-only').text()).toBe('1 Treffer')
      await search.setValue('gibtesnicht')
      expect(wrapper.find('table').exists()).toBe(false)
      expect(wrapper.find('.empty-state__title').text()).toBe('Kein Vorfall gefunden.')
    })

    it('filtert per Umschalter nach offenen und behobenen Vorfällen', async () => {
      const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: manyIncidents() }) })
      const seg = (text) => wrapper.findAll('.segment__item').find(b => b.text() === text)
      expect(wrapper.findAll('tbody tr')).toHaveLength(2)
      await seg('Offen').trigger('click')
      expect(wrapper.findAll('tbody tr').map(r => r.text())).toEqual([expect.stringContaining('API down')])
      expect(seg('Offen').attributes('aria-pressed')).toBe('true')
      await seg('Behoben').trigger('click')
      expect(wrapper.findAll('tbody tr').map(r => r.text())).toEqual([expect.stringContaining('Login langsam')])
    })

    it('Altwert completed zählt nicht mehr als behoben, die Migration stellt ihn um', async () => {
      const items = [...manyIncidents(), { ID: 3, Title: 'Alt', Message: 'x', Status: 'completed', Components: [], Tags: [], CreatedAt: '2026-07-03T10:00:00Z' }]
      const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: items }) })
      const seg = (text) => wrapper.findAll('.segment__item').find(b => b.text() === text)
      await seg('Offen').trigger('click')
      expect(wrapper.findAll('tbody tr')).toHaveLength(2)
      expect(wrapper.findAll('tbody tr').map(r => r.text()).join(' ')).toContain('Alt')
      await seg('Behoben').trigger('click')
      expect(wrapper.findAll('tbody tr').map(r => r.text())).toEqual([expect.stringContaining('Login langsam')])
      expect(wrapper.find('.page-head__subtitle').text()).toContain('2 offen')
    })

    it('bearbeiten: Formular steht als Seitenkarte, Titel nennt den Namen, × schließt', async () => {
      setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
      const { wrapper } = await mountAdminIncidents()
      await clickMenuItem(wrapper, 'Bearbeiten')
      await flushPromises()

      expect(wrapper.find('.split').classes()).toContain('split--open')
      expect(wrapper.find('table').exists()).toBe(true) // Liste bleibt neben dem Formular
      expect(wrapper.find('.form-panel h3').text()).toBe('Vorfall „API down“ bearbeiten')
      expect(wrapper.find('.form-panel__hint').text()).toBe('Felder mit * sind Pflichtfelder.')
      await wrapper.find('.form-panel__close').trigger('click')
      expect(wrapper.find('.form-panel').exists()).toBe(false)
    })

    it('Pflichtfelder: Fehler am Feld statt Request, Fokus aufs erste Feld, Fehler verschwindet beim Tippen', async () => {
      setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
      await wrapper.find('.page-head button').trigger('click')
      await wrapper.find('.edit-form').trigger('submit')
      await flushPromises()

      expect(wrapper.findAll('.form-field__message--error').map(e => e.text())).toEqual([
        expect.stringContaining('Titel ist ein Pflichtfeld.'),
        expect.stringContaining('Nachricht / Details ist ein Pflichtfeld.'),
        expect.stringContaining('Betroffene Komponente(n) ist ein Pflichtfeld.'),
      ])
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
      expect(fieldByLabel(wrapper, 'Titel').attributes('aria-invalid')).toBe('true')
      await fieldByLabel(wrapper, 'Titel').setValue('x')
      expect(fieldByLabel(wrapper, 'Titel').attributes('aria-invalid')).toBeUndefined()
    })

    // Regression: Die Komponenten-Auswahl (Pflicht) wurde nur vom Server
    // geprüft; jetzt steht der Fehler am Feld, der Label-Bezug zeigt aufs Feld.
    it('Komponenten-Pflichtfeld: Fehler am Feld ohne Request, verschwindet bei Auswahl', async () => {
      setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
      await wrapper.find('.page-head button').trigger('click')
      await fieldByLabel(wrapper, 'Titel').setValue('T')
      await fieldByLabel(wrapper, 'Nachricht / Details').setValue('M')
      await wrapper.find('.edit-form').trigger('submit')
      await flushPromises()

      expect(wrapper.findAll('.form-field__message--error').map(e => e.text())).toEqual([
        expect.stringContaining('Betroffene Komponente(n) ist ein Pflichtfeld.'),
      ])
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
      const label = wrapper.findAll('label.form-field__label').find(l => l.text() === 'Betroffene Komponente(n)')
      const field = wrapper.find(`[id="${label.attributes('for')}"]`)
      expect(field.exists()).toBe(true)
      expect(field.attributes('aria-invalid')).toBe('true')
      await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
      expect(wrapper.find('.form-field__message--error').exists()).toBe(false)
      expect(wrapper.find(`[id="${label.attributes('for')}"]`).attributes('aria-invalid')).toBeUndefined()
    })

    it('sucht auch in Einträgen ohne Nachricht und ohne Komponenten', async () => {
      const items = [{ ID: 3, Title: 'Nur Titel', Message: null, Status: 'monitoring', Components: null, Tags: [], CreatedAt: '2026-07-02T10:00:00Z' }]
      const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: items }) })
      await wrapper.find('input[type="search"]').setValue('titel')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      await wrapper.find('input[type="search"]').setValue('null')
      expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    })
  })

  it('zeigt den Status als StatusBadge mit Text statt .badge und englischem Rohwert', async () => {
    const rows = ['investigating', 'identified', 'monitoring', 'resolved'].map((Status, i) => ({
      ID: i + 1, Title: `V${i}`, Message: '', Status, Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z',
    }))
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: rows }) })

    const badges = wrapper.findAll('tbody .st-badge')
    expect(badges.map(b => b.text())).toEqual(['Untersuchung', 'Identifiziert', 'Beobachtung', 'Behoben'])
    expect(badges.map(b => b.classes().find(c => /^st-badge--(major|partial|degraded|operational)$/.test(c))))
      .toEqual(['st-badge--major', 'st-badge--partial', 'st-badge--degraded', 'st-badge--operational'])
    expect(wrapper.find('.badge').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('investigating')
  })

  it('zeigt einen unbekannten Status neutral mit Rohwert', async () => {
    const rows = [{ ID: 1, Title: 'X', Message: '', Status: 'seltsam', Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' }]
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: rows }) })

    const badge = wrapper.find('tbody .st-badge')
    expect(badge.text()).toBe('seltsam')
    expect(badge.classes()).toContain('st-badge--unknown')
  })

  it('nutzt den gemeinsamen Tabellenstil und zeigt Komponenten bzw. „—“', async () => {
    const rows = [
      { ID: 1, Title: 'A', Message: 'm', Status: 'resolved', Components: [{ ID: 1, Name: 'API' }, { ID: 2, Name: 'Web' }], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' },
      { ID: 2, Title: 'B', Message: '', Status: 'resolved', Components: null, Tags: [], CreatedAt: '2026-07-01T10:00:00Z' },
    ]
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ incidentsFixtureOverride: rows }) })

    expect(wrapper.find('.data-table-wrapper table.data-table').exists()).toBe(true)
    const cells = wrapper.findAll('tbody tr').map(r => r.find('td[data-label="Komponente(n)"]').text())
    expect(cells).toEqual(['API, Web', '—'])
    expect(wrapper.find('td[data-label="Titel"] a').attributes('href')).toBe('/admin/incidents/1/1')
  })

  it('beschriftet Formularfelder über FormField: Pflicht mit Stern, optionale mit „optional“', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter({ pageTags: [] }) })
    await wrapper.find('.page-head button').trigger('click')

    const title = fieldByLabel(wrapper, 'Titel')
    expect(title.attributes('required')).toBeDefined()
    expect(fieldByLabel(wrapper, 'Beginn').attributes('required')).toBeUndefined()
    const required = wrapper.findAll('.form-field').filter(f => f.find('.form-field__required').exists())
    expect(required.map(f => f.find('label').text())).toEqual(['Titel', 'Nachricht / Details', 'Status', 'Betroffene Komponente(n)'])
    const optional = wrapper.findAll('.form-field').filter(f => f.find('.form-field__optional').exists())
    expect(optional.map(f => f.find('label').text())).toEqual(['Vorlage verwenden', 'Schweregrad', 'Beginn', 'Ende'])
  })

  it('bietet die Status mit deutschen Beschriftungen an und sendet den Rohwert', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminIncidents({ fetchImpl: fetchMock })
    await wrapper.find('.page-head button').trigger('click')

    const select = fieldByLabel(wrapper, 'Status')
    expect(select.findAll('option').map(o => o.text())).toEqual(['Untersuchung', 'Identifiziert', 'Beobachtung', 'Behoben'])
    await select.setValue('monitoring')
    await fieldByLabel(wrapper, 'Titel').setValue('T')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('M')
    await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    const post = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(JSON.parse(post[1].body).Status).toBe('monitoring')
  })

  it('wendet eine Vorlage nur bei echter Auswahl an (leere Auswahl lässt Felder unberührt)', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()
    await wrapper.find('.page-head button').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Eigener Titel')

    await fieldByLabel(wrapper, 'Vorlage verwenden').setValue('')
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Eigener Titel')
  })

  it('hat keine Inline-Styles und keine Hex-Farben', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    const { wrapper } = await mountAdminIncidents()
    expectNoInlineStyles(wrapper, import.meta.url, 'AdminIncidents.vue')

    await clickMenuItem(wrapper, 'Bearbeiten')
    expectNoInlineStyles(wrapper, import.meta.url, 'AdminIncidents.vue')
  })

  // Ohne Recht zum Ändern öffnet „Ansehen“ das Formular gesperrt, ohne Speichern.
  it('öffnet das Formular ohne Änderungsrecht gesperrt und ohne Speichern-Knopf', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:read' })
    const { wrapper } = await mountAdminIncidents({ fetchImpl: makeFetchRouter() })
    await wrapper.findAll('[role="menuitem"]').find(i => i.text() === 'Ansehen').trigger('click')
    await flushPromises()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.form-lock-hint').text()).toContain('Nur Lesezugriff')
    expect(wrapper.findAll('button[type="submit"]')).toHaveLength(0)
  })
  // Löschen blieb ohne Rückmeldung, eine ältere Meldung stand weiter da.
  it('meldet das Löschen als Erfolg und entfernt die ältere Meldung', async () => {
    setAuthAsPageRole('jwt-1', { permissions: INCIDENT_PERMS })
    stubConfirm(true)
    const { wrapper } = await mountAdminIncidents()
    wrapper.vm.showAlert('Vorfall erstellt.', 'success')
    await flushPromises()

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Vorfall erstellt.')
    expect(wrapper.find('.alert-success').text()).toContain('Gelöscht.')
  })
})
