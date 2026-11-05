import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminExternalSources from './AdminExternalSources.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'

// Admin-Seite „Externe Quellen“.

const MINUTE = 60 * 1000
const ago = (ms) => new Date(Date.now() - ms).toISOString()

function sourceFixture(over = {}) {
  return {
    ID: 1, name: 'Example Cloud', base_url: 'https://status.example.com', provider: 'summaryv2', active: true,
    poll_interval_seconds: 120, outage_after_minutes: 60, last_fetch_at: ago(2 * MINUTE), last_fetch_ok: true,
    last_success_at: ago(2 * MINUTE), last_error: '', component_count: 113, link_count: 2,
    outage: { active: false, since: null, last_error: '' }, ...over,
  }
}
const linkFixture = (over = {}) => ({
  id: 4, component_id: 1, component_name: 'API', external_component_id: 7, external_component_name: 'VM',
  group_name: 'Compute', source_id: 1, source_name: 'Example Cloud', status: 'partial_outage', vanished: false,
  is_group: false, warnings: [], ...over,
})
const incidentFixture = (over = {}) => ({
  id: 9, source_id: 1, source_name: 'Example Cloud', kind: 'incident', title: 'Netzwerkstörung', status: 'investigating',
  raw_status: 'investigating', closed: false, url: 'https://status.example.com/incidents/abc', updated_at: ago(5 * MINUTE),
  affected_components: [{ id: 1, name: 'API' }], external_components: [],
  updates: [{ id: 'u1', status: 'investigating', body: 'Wir untersuchen.', display_at: ago(10 * MINUTE) }], ...over,
})
const extComponents = {
  groups: [{ ID: 20, name: 'Database', is_group: true }],
  components: [
    { ID: 20, name: 'Database', is_group: true, group_name: '' },
    { ID: 7, name: 'VM', is_group: false, group_name: 'Compute' },
    { ID: 8, name: 'Postgres', is_group: false, group_name: 'Database' },
    { ID: 9, name: 'Solo', is_group: false, group_name: '' },
  ],
}

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })

function makeFetch({ sources = [sourceFixture()], links = [linkFixture()], incidents = [incidentFixture()], handlers = {} } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    const key = `${method} ${url}`
    for (const [pattern, fn] of Object.entries(handlers)) {
      if (new RegExp(pattern).test(key)) return fn(url, options)
    }
    if (method === 'GET' && /external-sources\/\d+\/components$/.test(url)) return json(extComponents)
    if (method === 'GET' && /external-sources$/.test(url)) return json(sources)
    if (method === 'GET' && /external-links$/.test(url)) return json(links)
    if (method === 'GET' && /external-incidents/.test(url)) return json(incidents)
    if (method === 'GET' && /\/components$/.test(url)) return json([{ ID: 1, Name: 'API', Status: 'operational' }, { ID: 2, Name: 'Gruppe', IsGroup: true, Status: 'group', Children: [] }])
    return json({})
  })
}

const calls = (method, re) => global.fetch.mock.calls.filter(([u, o]) => (o?.method || 'GET') === method && re.test(u))
const body = (call) => JSON.parse(call[1].body)

let wrapper = null
// Tab-Rechte statt components:write; Übernehmen braucht zusätzlich components:create.
const FULL_PERMS = 'external_sources:create,external_sources:update,external_sources:delete,components:create'

async function mountView({ perms = FULL_PERMS, fetchImpl, tenantId = '1' } = {}) {
  setAuthAsPageRole('jwt-1', { permissions: perms, is_global_admin: false })
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetch()
  wrapper = mount(AdminExternalSources, {
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } },
  })
  await flushPromises()
  return wrapper
}

const buttonByText = (text) => wrapper.findAll('button').find(b => b.text().includes(text))
const menuItem = (text) => wrapper.findAll('[role="menuitem"]').find(i => i.text().includes(text))
const fieldByLabel = (text) => {
  const label = wrapper.findAll('label').find(l => l.text().startsWith(text))
  return wrapper.find(`#${label.attributes('for')}`)
}

describe('AdminExternalSources', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('zeigt Quelle mit Zustand, letztem Abruf, Zählern kleinen Kennzahlen und lädt nur die aktive Statusseite', async () => {
    await mountView()
    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('Example Cloud')
    expect(row.text()).toContain('https://status.example.com')
    expect(row.text()).toContain('Erreichbar')
    expect(row.text()).toContain('vor 2 Minuten')
    expect(row.text()).toContain('113')
    expect(wrapper.text()).toContain('Quellen im Ausfall')
    const urls = global.fetch.mock.calls.map(c => c[0])
    expect(urls).toContain('/api/pages/1/external-sources')
    expect(urls).toContain('/api/pages/1/external-links')
    expect(urls.every(u => u.startsWith('/api/pages/1/'))).toBe(true)
  })

  it('lädt beim Mandantenwechsel neu für die andere Statusseite', async () => {
    await mountView()
    useTenant().setActiveTenant('2')
    await flushPromises()
    expect(calls('GET', /\/api\/pages\/2\/external-sources$/)).toHaveLength(1)
  })

  it('kennzeichnet einen Ausfall mit Fehlertext, ohne HTML der Fremdseite zu rendern', async () => {
    await mountView({ fetchImpl: makeFetch({ sources: [sourceFixture({
      last_fetch_ok: false, last_error: '<img src=x onerror=alert(1)> HTTP 503',
      outage: { active: true, since: ago(90 * MINUTE), last_error: 'x' },
    })] }) })
    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('Ausfall')
    expect(row.text()).toContain('Keine Daten seit')
    expect(row.text()).toContain('<img src=x onerror=alert(1)> HTTP 503')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('Quellen im Ausfall')
    expect(wrapper.findAll('.kpi--alert')).toHaveLength(1)
  })

  it.each([
    [{ active: false }, 'Inaktiv'],
    [{ last_fetch_ok: false, last_error: 'HTTP 500' }, 'Abruf fehlgeschlagen'],
    [{ last_fetch_ok: null, last_fetch_at: null, last_success_at: null }, 'Noch nicht abgerufen'],
  ])('zeigt den Zustand %j als „%s“', async (over, label) => {
    await mountView({ fetchImpl: makeFetch({ sources: [sourceFixture(over)] }) })
    expect(wrapper.find('tbody tr').text()).toContain(label)
  })

  it('zeigt gespiegelte Update-Texte nur als Text, nie als HTML', async () => {
    const evil = '<script>window.__x=1</script><b>fett</b>'
    await mountView({ fetchImpl: makeFetch({ incidents: [incidentFixture({
      title: '<i>Titel</i>', updates: [{ id: 'u1', status: 'x', body: evil, display_at: ago(MINUTE) }],
    })] }) })
    const details = wrapper.find('details')
    expect(details.text()).toContain(evil)
    expect(details.find('b').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('<i>Titel</i>')
    expect(wrapper.find('i').exists()).toBe(false)
    expect(wrapper.find('a[href="https://status.example.com/incidents/abc"]').attributes('rel')).toContain('noopener')
  })

  // Stand-Zelle zeigte „investigatingOffen“ in einer Zeile, weil der Fremdtext
  // und das übersetzte Zustandswort ohne Trennung aneinanderklebten.
  it('trennt in der Stand-Zelle das Zustandswort vom Fremdtext der Quelle', async () => {
    await mountView()
    const cell = wrapper.find('[data-testid="ext-raw-status"]')
    expect(cell.exists()).toBe(true)
    expect(cell.text()).toBe('investigating')
    expect(cell.classes()).toContain('cell-sub')
    expect(cell.element.parentElement.textContent.replace(cell.text(), '').trim()).toBe('Offen')
  })

  it('filtert gespiegelte Meldungen nach Art, Zustand und Quelle über die API', async () => {
    await mountView()
    await fieldByLabel('Art').setValue('maintenance')
    await flushPromises()
    await fieldByLabel('Zustand').setValue('open')
    await flushPromises()
    const urls = calls('GET', /external-incidents/).map(c => c[0])
    expect(urls.at(-1)).toContain('kind=maintenance')
    expect(urls.at(-1)).toContain('state=open')
    await wrapper.findAll('select').at(-1).setValue('1')
    await flushPromises()
    expect(calls('GET', /external-incidents/).at(-1)[0]).toContain('source_id=1')
  })

  it('zeigt Leerzustände und einen Ladefehler der Meldungen', async () => {
    await mountView({ fetchImpl: makeFetch({ sources: [], links: [], incidents: [] }) })
    expect(wrapper.text()).toContain('Noch keine externe Quelle')
    expect(wrapper.text()).toContain('Noch keine Kopplung')
    expect(wrapper.text()).toContain('Keine gespiegelten Meldungen')
    wrapper.unmount()
    await mountView({ fetchImpl: makeFetch({ incidents: [], handlers: { 'external-incidents': () => json({ error: 'kaputt' }, 500) } }) })
    expect(wrapper.text()).toContain('Die Liste konnte nicht geladen werden.')
  })

  it('zeigt bei 403 die Berechtigungsmeldung und keine Daten', async () => {
    await mountView({ fetchImpl: makeFetch({ handlers: { 'external-sources$': () => json({ error: 'Forbidden' }, 403) } }) })
    expect(wrapper.text()).toContain('Keine Berechtigung')
    expect(wrapper.find('table').exists()).toBe(false)
  })

  it('zeigt bei einem Ladefehler eine Meldung', async () => {
    await mountView({ fetchImpl: makeFetch({ handlers: { 'external-links$': () => json({ error: 'kaputt' }, 500) } }) })
    expect(wrapper.find('.alert-error').text()).toContain('Fehler beim Laden')
  })

  describe('Quelle anlegen und bearbeiten', () => {
    it('prüft Pflichtfelder, ohne den Server zu fragen', async () => {
      await mountView()
      await buttonByText('Neue Quelle').trigger('click')
      await wrapper.find('form.edit-form').trigger('submit')
      await flushPromises()
      expect(wrapper.text()).toContain('Bitte einen Namen eingeben.')
      expect(wrapper.text()).toContain('Bitte die Adresse der Statusseite eingeben.')
      expect(calls('POST', /external-sources$/)).toHaveLength(0)
    })

    it('nutzt die gemeinsame Seitenkarte: Hinweis, Schließen-Knopf, Fokus aufs Fehlerfeld', async () => {
      await mountView()
      await buttonByText('Neue Quelle').trigger('click')
      expect(wrapper.find('.split').classes()).toContain('split--open')
      expect(wrapper.find('aside.form-panel .form-panel__hint').text()).toBe('Felder mit * sind Pflichtfelder.')
      await wrapper.find('form.edit-form').trigger('submit')
      await flushPromises()
      expect(document.activeElement).toBe(fieldByLabel('Name').element)
      await fieldByLabel('Name').setValue('X')
      expect(wrapper.text()).not.toContain('Bitte einen Namen eingeben.')
      expect(wrapper.text()).toContain('Bitte die Adresse der Statusseite eingeben.')
      await wrapper.find('.form-panel__close').trigger('click')
      expect(wrapper.find('aside').exists()).toBe(false)
      expect(wrapper.find('.split').classes()).not.toContain('split--open')
    })

    it('legt eine Quelle mit den Standardwerten an', async () => {
      await mountView()
      await buttonByText('Neue Quelle').trigger('click')
      await fieldByLabel('Name').setValue('GitHub')
      await fieldByLabel('Adresse der Statusseite').setValue('https://www.githubstatus.com')
      await wrapper.find('form.edit-form').trigger('submit')
      await flushPromises()
      const [post] = calls('POST', /\/api\/pages\/1\/external-sources$/)
      expect(body(post)).toEqual({ name: 'GitHub', base_url: 'https://www.githubstatus.com', poll_interval_seconds: 120, outage_after_minutes: 60, active: true })
      expect(wrapper.text()).toContain('Quelle angelegt.')
      expect(wrapper.find('aside').exists()).toBe(false)
    })

    it('zeigt den Fehler des Servers (z. B. SSRF-Schutz) und lässt das Formular offen', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { '^POST .*external-sources$': () => json({ error: 'invalid base_url: private address' }, 400) } }) })
      await buttonByText('Neue Quelle').trigger('click')
      await fieldByLabel('Name').setValue('Intern')
      await fieldByLabel('Adresse der Statusseite').setValue('https://10.0.0.1')
      await wrapper.find('form.edit-form').trigger('submit')
      await flushPromises()
      expect(wrapper.find('.alert-error').text()).toContain('private address')
      expect(wrapper.find('aside').exists()).toBe(true)
    })

    it('bearbeitet eine Quelle per PUT', async () => {
      await mountView()
      await menuItem('Bearbeiten').trigger('click')
      expect(wrapper.find('aside h3').text()).toContain('Example Cloud')
      await fieldByLabel('Abruf-Intervall').setValue('300')
      await fieldByLabel('Aktiv').setValue(false)
      await wrapper.find('form.edit-form').trigger('submit')
      await flushPromises()
      const [put] = calls('PUT', /external-sources\/1$/)
      expect(body(put)).toMatchObject({ name: 'Example Cloud', poll_interval_seconds: 300, outage_after_minutes: 60, active: false })
    })

    it('Abbrechen schließt das Formular ohne Anfrage', async () => {
      await mountView()
      await menuItem('Bearbeiten').trigger('click')
      await buttonByText('Abbrechen').trigger('click')
      expect(wrapper.find('aside').exists()).toBe(false)
      expect(calls('PUT', /external-sources/)).toHaveLength(0)
    })
  })

  describe('Löschen und Abrufen', () => {
    it('löscht nach Bestätigung, nicht ohne', async () => {
      await mountView()
      stubConfirm(false)
      await menuItem('Löschen').trigger('click')
      expect(calls('DELETE', /external-sources/)).toHaveLength(0)
      stubConfirm(true)
      await menuItem('Löschen').trigger('click')
      await flushPromises()
      expect(calls('DELETE', /\/api\/pages\/1\/external-sources\/1$/)).toHaveLength(1)
      vi.unstubAllGlobals()
    })

    it('zeigt einen Löschfehler', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { '^DELETE': () => json({ error: 'nein' }, 500) } }) })
      stubConfirm(true)
      await menuItem('Löschen').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-error').text()).toContain('Löschen fehlgeschlagen')
      vi.unstubAllGlobals()
    })

    it('ruft „Jetzt abrufen“ auf und meldet Erfolg', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { '^POST .*/fetch$': () => json({ result: { ok: true }, source: sourceFixture() }) } }) })
      await menuItem('Jetzt abrufen').trigger('click')
      await flushPromises()
      expect(calls('POST', /\/api\/pages\/1\/external-sources\/1\/fetch$/)).toHaveLength(1)
      expect(wrapper.find('.alert-success').text()).toContain('„Example Cloud“ wurde abgerufen.')
    })

    it('meldet den Fehler der Fremdseite und eine laufende Sperre', async () => {
      let result = { ok: false, error: 'HTTP 503' }
      await mountView({ fetchImpl: makeFetch({ handlers: { '^POST .*/fetch$': () => json({ result, source: sourceFixture() }) } }) })
      await menuItem('Jetzt abrufen').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-error').text()).toContain('HTTP 503')
      result = { ok: false, error: 'fetch_in_progress' }
      await menuItem('Jetzt abrufen').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-warning').text()).toContain('läuft gerade schon ein Abruf')
    })

    it('meldet einen Anfragefehler beim Abrufen', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { '^POST .*/fetch$': () => json({ error: 'Too many requests' }, 429) } }) })
      await menuItem('Jetzt abrufen').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-error').text()).toContain('Abruf fehlgeschlagen')
    })
  })

  describe('Kopplungen', () => {
    it('listet Kopplungen mit Status, Gruppe, Warnung und schlechtestem Baustein', async () => {
      await mountView({ fetchImpl: makeFetch({ links: [
        linkFixture(),
        linkFixture({ id: 5, external_component_id: 20, external_component_name: 'Database', is_group: true, group_name: 'Database', status: 'major_outage', worst: { name: 'Postgres', status: 'major_outage' }, warnings: ['overlaps_group'] }),
        linkFixture({ id: 6, external_component_name: 'Alt', vanished: true }),
        linkFixture({ id: 7, external_component_name: 'Neu', status: 'unknown' }),
      ] }) })
      const rows = wrapper.findAll('section.list-card')[2].findAll('tbody tr')
      expect(rows[0].text()).toContain('Teilausfall')
      expect(rows[0].text()).toContain('Compute')
      expect(rows[1].text()).toContain('Ganze Gruppe')
      expect(rows[1].text()).toContain('Schlechtester Baustein: Postgres')
      expect(rows[1].text()).toContain('doppelt gekoppelt')
      expect(rows[2].text()).toContain('Verschwunden')
      expect(rows[3].text()).toContain('Unbekannt')
    })

    it('legt eine Kopplung an: Quelle wählen lädt Bausteine, Gruppen sind kenntlich, Suche filtert', async () => {
      await mountView()
      expect(fieldByLabel('Externer Baustein').attributes('disabled')).toBeDefined()
      await fieldByLabel('Eigene Komponente').setValue('1')
      await wrapper.find('.ext-link-form select:nth-of-type(1)').exists()
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      expect(calls('GET', /external-sources\/1\/components$/)).toHaveLength(1)
      const options = () => fieldByLabel('Externer Baustein').findAll('option').map(o => o.text())
      expect(options()).toContain('Ganze Gruppe: Database')
      expect(options()).toContain('Compute › VM')
      expect(options()).toContain('Database › Postgres')
      expect(options()).toContain('Solo')
      await fieldByLabel('Baustein suchen').setValue('postgres')
      expect(options()).toEqual(['Bitte wählen', 'Database › Postgres'])
      await fieldByLabel('Externer Baustein').setValue('8')
      await buttonByText('Koppeln').trigger('click')
      await wrapper.find('.ext-link-form').trigger('submit')
      await flushPromises()
      const [post] = calls('POST', /\/api\/pages\/1\/external-links$/)
      expect(body(post)).toEqual({ component_id: 1, external_component_id: 8 })
      expect(wrapper.text()).toContain('Kopplung angelegt.')
    })

    it('bietet reine Gruppen der eigenen Seite nicht zur Kopplung an', async () => {
      await mountView()
      const options = fieldByLabel('Eigene Komponente').findAll('option').map(o => o.text())
      expect(options).toEqual(['Bitte wählen', 'API'])
    })

    it('verlangt beide Seiten der Kopplung, ohne den Server zu fragen', async () => {
      await mountView()
      await wrapper.find('.ext-link-form').trigger('submit')
      expect(wrapper.text()).toContain('Bitte eine eigene Komponente wählen.')
      expect(wrapper.text()).toContain('Bitte einen externen Baustein wählen.')
      expect(calls('POST', /external-links/)).toHaveLength(0)
    })

    it('zeigt Fehler der Kopplung (z. B. 400) und der Bausteinliste', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: {
        '^POST .*external-links$': () => json({ error: 'Group components cannot be linked' }, 400),
        'external-sources/1/components$': () => json({ error: 'kaputt' }, 500),
      } }) })
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      expect(wrapper.text()).toContain('Die Liste konnte nicht geladen werden.')
      await fieldByLabel('Eigene Komponente').setValue('1')
      wrapper.vm.$.setupState.linkExternalId = '8'
      await wrapper.find('.ext-link-form').trigger('submit')
      await flushPromises()
      expect(wrapper.find('.alert-error').text()).toContain('Group components cannot be linked')
    })

    it('löst eine Kopplung nach Bestätigung', async () => {
      await mountView()
      stubConfirm(false)
      await buttonByText('Lösen').trigger('click')
      expect(calls('DELETE', /external-links/)).toHaveLength(0)
      stubConfirm(true)
      await buttonByText('Lösen').trigger('click')
      await flushPromises()
      expect(calls('DELETE', /\/api\/pages\/1\/external-links\/4$/)).toHaveLength(1)
      vi.unstubAllGlobals()
    })

    it('zeigt einen Fehler beim Lösen', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { '^DELETE': () => json({ error: 'nein' }, 500) } }) })
      stubConfirm(true)
      await buttonByText('Lösen').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-error').text()).toContain('Löschen fehlgeschlagen')
      vi.unstubAllGlobals()
    })
  })

  describe('Randfälle', () => {
    it('zeigt den Namen der Statusseite in den Brotkrumen', async () => {
      useTenant().setMyPages([{ ID: 1, Name: 'Acme Status' }])
      await mountView()
      expect(wrapper.text()).toContain('Acme Status')
    })

    it('behandelt null-Antworten der API wie leere Listen', async () => {
      await mountView({ fetchImpl: vi.fn(async () => json(null)) })
      expect(wrapper.text()).toContain('Noch keine externe Quelle')
      expect(wrapper.text()).toContain('Keine gespiegelten Meldungen')
    })

    it('formuliert Intervalle in Minuten und Sekunden', async () => {
      await mountView({ fetchImpl: makeFetch({ sources: [
        sourceFixture({ ID: 1, poll_interval_seconds: 60 }),
        sourceFixture({ ID: 2, name: 'B', base_url: 'https://b.example', poll_interval_seconds: 90 }),
        sourceFixture({ ID: 3, name: 'C', base_url: 'https://c.example', poll_interval_seconds: 120 }),
      ] }) })
      const rows = wrapper.findAll('section.list-card')[0].findAll('tbody tr')
      expect(rows[0].text()).toContain('Abruf jede Minute')
      expect(rows[1].text()).toContain('Abruf alle 90 Sekunden')
      expect(rows[2].text()).toContain('Abruf alle 2 Minuten')
    })

    it('übernimmt den Ausfall-Schwellwert beim Bearbeiten', async () => {
      await mountView()
      await menuItem('Bearbeiten').trigger('click')
      await fieldByLabel('Ausfall nach').setValue('90')
      await wrapper.find('form.edit-form').trigger('submit')
      await flushPromises()
      expect(body(calls('PUT', /external-sources\/1$/)[0]).outage_after_minutes).toBe(90)
    })

    it('zeigt Wartung, beendete Meldung ohne Link und ohne Änderungszeit sowie das Roh-Stand-Fallback', async () => {
      await mountView({ fetchImpl: makeFetch({ incidents: [incidentFixture({
        id: 11, kind: 'maintenance', closed: true, url: '', updated_at: null, raw_status: '', status: 'completed', updates: [],
      })] }) })
      const row = wrapper.findAll('section.list-card')[3].find('tbody tr')
      expect(row.text()).toContain('Wartung')
      expect(row.text()).toContain('completed')
      expect(row.text()).toContain('Beendet')
      expect(row.find('a').exists()).toBe(false)
      expect(row.find('details').exists()).toBe(false)
      expect(row.text()).toContain('—')
    })

    it('schließt die Meldung per Knopf', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { '^DELETE': () => json({ error: 'nein' }, 500) } }) })
      stubConfirm(true)
      await menuItem('Löschen').trigger('click')
      await flushPromises()
      await wrapper.find('.alert-close').trigger('click')
      expect(wrapper.find('.alert').exists()).toBe(false)
      vi.unstubAllGlobals()
    })

    it('setzt die Quellenwahl der Kopplung zurück, wenn diese Quelle gelöscht wird', async () => {
      await mountView()
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      stubConfirm(true)
      await menuItem('Löschen').trigger('click')
      await flushPromises()
      expect(fieldByLabel('Quelle').element.value).toBe('')
      vi.unstubAllGlobals()
    })

    it('leert die Bausteinliste, wenn die Quelle wieder abgewählt wird', async () => {
      await mountView()
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      await fieldByLabel('Quelle').setValue('')
      await flushPromises()
      expect(calls('GET', /external-sources\/\d+\/components$/)).toHaveLength(1)
      expect(fieldByLabel('Externer Baustein').findAll('option')).toHaveLength(1)
    })

    it('lädt nichts ohne aktive Statusseite', async () => {
      await mountView()
      const before = global.fetch.mock.calls.length
      useTenant().setActiveTenant(null)
      await flushPromises()
      expect(global.fetch.mock.calls.length).toBe(before)
    })

    it('toleriert eine Antwort der Bausteinliste ohne components', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { 'external-sources/1/components$': () => json({}) } }) })
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      expect(fieldByLabel('Externer Baustein').findAll('option')).toHaveLength(1)
    })
  })

  // Bausteine samt eigener Komponente übernehmen.
  describe('Bausteine übernehmen', () => {
    const importList = {
      groups: [],
      components: [
        { ID: 20, external_id: 'g-db', name: 'Database', is_group: true, group_name: '', group_external_id: '', linked_component_ids: [] },
        { ID: 7, external_id: 'vm', name: 'VM', is_group: false, group_name: 'Compute', group_external_id: 'g-c', linked_component_ids: [1] },
        { ID: 8, external_id: 'pg', name: 'Postgres', is_group: false, group_name: 'Database', group_external_id: 'g-db', linked_component_ids: [] },
        { ID: 9, external_id: 'solo', name: 'Solo', is_group: false, group_name: '', group_external_id: '', linked_component_ids: [] },
        { ID: 21, external_id: 'g-c', name: 'Compute', is_group: true, group_name: '', group_external_id: '', linked_component_ids: [] },
      ],
    }
    const tree = [
      { ID: 1, Name: 'API', Status: 'operational' },
      { ID: 2, Name: 'Cloud', IsGroup: true, Status: 'group', Children: [{ ID: 3, Name: 'Region', IsGroup: true, Status: 'group', Children: [] }] },
    ]
    const importHandlers = (extra = {}) => ({
      'external-sources/1/components$': () => json(importList),
      '^GET /api/pages/1/components$': () => json(tree),
      ...extra,
    })
    const importForm = () => wrapper.find('.ext-import')
    const rowByName = (name) => wrapper.findAll('.ext-import__row').find(r => r.find('strong').text() === name)
    const importCheckbox = (name) => rowByName(name).find('input')
    const chooseSource = async () => {
      await fieldByLabel('Externe Quelle').setValue('1')
      await flushPromises()
    }
    const submitImport = async () => {
      await importForm().trigger('submit')
      await flushPromises()
    }

    it('zeigt die Karte nur mit external_sources:create und components:create und mindestens einer Quelle', async () => {
      await mountView({ perms: 'external_sources:read' })
      expect(wrapper.find('.ext-import').exists()).toBe(false)
      wrapper.unmount()
      await mountView({ fetchImpl: makeFetch({ sources: [], links: [] }) })
      expect(wrapper.find('.ext-import').exists()).toBe(false)
      wrapper.unmount()
      await mountView()
      expect(wrapper.text()).toContain('Bausteine übernehmen')
      expect(wrapper.find('.ext-import__list').exists()).toBe(false)
    })

    it('listet Bausteine mit Kennzeichnung: Gruppen, bereits gekoppelte nicht wählbar', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers() }) })
      await chooseSource()
      expect(calls('GET', /external-sources\/1\/components$/)).toHaveLength(1)
      expect(wrapper.findAll('.ext-import__row')).toHaveLength(5)
      expect(rowByName('Database').text()).toContain('Ganze Gruppe')
      expect(rowByName('VM').text()).toContain('Bereits gekoppelt')
      expect(importCheckbox('VM').attributes('disabled')).toBeDefined()
      expect(importCheckbox('Postgres').attributes('disabled')).toBeUndefined()
      expect(importCheckbox('Database').attributes('disabled')).toBeUndefined()
    })

    it('sperrt eine Gruppe, deren Bausteine alle gekoppelt sind', async () => {
      const list = JSON.parse(JSON.stringify(importList))
      list.components.find(c => c.ID === 8).linked_component_ids = [1]
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ 'external-sources/1/components$': () => json(list) }) }) })
      await chooseSource()
      expect(importCheckbox('Database').attributes('disabled')).toBeDefined()
      expect(rowByName('Database').text()).toContain('Bereits gekoppelt')
      // Eine Gruppe ohne Mitglieder in der Liste bleibt wählbar (das Backend meldet sie dann als leer).
      expect(importCheckbox('Compute').attributes('disabled')).toBeDefined()
    })

    it('durchsucht Name und Gruppenname und wählt alle sichtbaren, nicht gekoppelten Bausteine', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers() }) })
      await chooseSource()
      await fieldByLabel('Bausteine durchsuchen').setValue('database')
      expect(wrapper.findAll('.ext-import__row').map(r => r.find('strong').text())).toEqual(['Database', 'Postgres'])
      await buttonByText('Alle sichtbaren wählen').trigger('click')
      expect(wrapper.find('.ext-import__count').text()).toBe('2 gewählt')
      // Compute und VM sind (über VM) gekoppelt: nichts Neues wählbar, die Auswahl von vorher bleibt.
      await fieldByLabel('Bausteine durchsuchen').setValue('compute')
      await buttonByText('Alle sichtbaren wählen').trigger('click')
      expect(wrapper.find('.ext-import__count').text()).toBe('2 gewählt')
      await fieldByLabel('Bausteine durchsuchen').setValue('solo')
      await buttonByText('Alle sichtbaren wählen').trigger('click')
      expect(wrapper.find('.ext-import__count').text()).toBe('3 gewählt')
      await buttonByText('Auswahl aufheben').trigger('click')
      expect(wrapper.find('.ext-import__count').text()).toBe('0 gewählt')
      await fieldByLabel('Bausteine durchsuchen').setValue('gibtsnicht')
      expect(wrapper.text()).toContain('Keine Bausteine gefunden.')
    })

    it('bietet als Ziel die oberste Ebene und alle eigenen Gruppen eingerückt an', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers() }) })
      const options = fieldByLabel('Zielgruppe').findAll('option').map(o => o.element.textContent)
      expect(options).toEqual(['Oberste Ebene', 'Cloud',' Region'])
    })

    it('übernimmt die Auswahl, meldet Angelegtes und Übersprungenes und lädt neu', async () => {
      const result = { created: [{ component_id: 10, name: 'Postgres', external_component_id: 8, is_group: false }], skipped: [{ external_component_id: 9, reason: 'already_linked' }] }
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ '^POST .*/external-sources/1/import$': () => json(result, 201) }) }) })
      await chooseSource()
      await importCheckbox('Postgres').setValue(true)
      await importCheckbox('Solo').setValue(true)
      await fieldByLabel('Zielgruppe').setValue('3')
      expect(buttonByText('Übernehmen (2)')).toBeDefined()
      const sourceLoads = calls('GET', /external-sources$/).length
      await submitImport()
      const [post] = calls('POST', /\/api\/pages\/1\/external-sources\/1\/import$/)
      expect(body(post)).toEqual({ external_component_ids: [8, 9], parent_id: 3, tag_ids: [] })
      expect(wrapper.find('.alert-success').text()).toContain('1 angelegt, 1 übersprungen')
      expect(wrapper.find('.ext-import__result').text()).toContain('Solo: bereits gekoppelt')
      expect(calls('GET', /external-sources$/).length).toBeGreaterThan(sourceLoads)
      expect(calls('GET', /external-links$/).length).toBeGreaterThan(1)
      expect(calls('GET', /external-sources\/1\/components$/)).toHaveLength(2)
      expect(wrapper.find('.ext-import__count').text()).toBe('0 gewählt')
    })

    it('schickt parent_id null für die oberste Ebene', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ '^POST .*/import$': () => json({ created: [], skipped: [] }, 200) }) }) })
      await chooseSource()
      await importCheckbox('Solo').setValue(true)
      await submitImport()
      expect(body(calls('POST', /import$/)[0]).parent_id).toBeNull()
      expect(wrapper.find('.alert-success').text()).toContain('0 angelegt, 0 übersprungen')
    })

    it('verlangt eine Auswahl, ohne den Server zu fragen', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers() }) })
      await submitImport()
      expect(wrapper.find('.ext-import__error').text()).toContain('Bitte eine Quelle wählen.')
      await chooseSource()
      await submitImport()
      expect(wrapper.find('.ext-import__error').text()).toContain('Bitte mindestens einen Baustein wählen.')
      expect(wrapper.find('.ext-import__error').attributes('role')).toBe('alert')
      expect(calls('POST', /import$/)).toHaveLength(0)
    })

    it('begrenzt die Menge pro Übernahme auf 200', async () => {
      const many = { groups: [], components: Array.from({ length: 201 }, (_, i) => ({ ID: 100 + i, external_id: `c${i}`, name: `Baustein ${i}`, is_group: false, group_name: '', group_external_id: '', linked_component_ids: [] })) }
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ 'external-sources/1/components$': () => json(many) }) }) })
      await chooseSource()
      await buttonByText('Alle sichtbaren wählen').trigger('click')
      await submitImport()
      expect(wrapper.find('.ext-import__error').text()).toContain('Höchstens 200 Bausteine pro Übernahme.')
      expect(calls('POST', /import$/)).toHaveLength(0)
    })

    it('zeigt Serverfehler am Formular und behält die Auswahl', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ '^POST .*/import$': () => json({ error: 'parent must be a group' }, 400) }) }) })
      await chooseSource()
      await importCheckbox('Solo').setValue(true)
      await submitImport()
      expect(wrapper.find('.ext-import__error').text()).toContain('Übernahme fehlgeschlagen: parent must be a group')
      expect(importCheckbox('Solo').element.checked).toBe(true)
      expect(wrapper.find('.alert-success').exists()).toBe(false)
    })

    it('verhindert einen zweiten Absender während die Übernahme läuft (Doppelklick)', async () => {
      let release
      const pending = new Promise(resolve => { release = resolve })
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ '^POST .*/import$': () => pending.then(() => json({ created: [], skipped: [] })) }) }) })
      await chooseSource()
      await importCheckbox('Solo').setValue(true)
      await importForm().trigger('submit')
      expect(buttonByText('Übernehme…').attributes('disabled')).toBeDefined()
      release()
      await flushPromises()
      expect(calls('POST', /import$/)).toHaveLength(1)
      expect(buttonByText('Übernehmen (0)').attributes('disabled')).toBeUndefined()
    })

    it('Tags: nur für Global Admins, die gewählten gehen mit', async () => {
      const handlers = importHandlers({
        '/api/tags': () => json([{ ID: 5, Name: 'EU' }, { ID: 6, Name: 'US' }]),
        '^POST .*/import$': () => json({ created: [], skipped: [] }),
      })
      await mountView({ fetchImpl: makeFetch({ handlers }) })
      expect(wrapper.find('.ext-import .tag-chips').exists()).toBe(false)
      expect(calls('GET', /\/api\/tags/)).toHaveLength(0)
      wrapper.unmount()
      setAuthAsPageRole('jwt-1', { permissions: FULL_PERMS, is_global_admin: true })
      useTenant().setActiveTenant('1')
      global.fetch = makeFetch({ handlers })
      wrapper = mount(AdminExternalSources, { attachTo: document.body, global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } } })
      await flushPromises()
      await chooseSource()
      await importCheckbox('Solo').setValue(true)
      await wrapper.find('.ext-import .tag-chips__add').setValue('5')
      await submitImport()
      expect(body(calls('POST', /import$/)[0]).tag_ids).toEqual([5])
    })

    it('setzt Auswahl und Ergebnis zurück, wenn die Quelle wechselt', async () => {
      const sources = [sourceFixture(), sourceFixture({ ID: 2, name: 'GitHub', base_url: 'https://www.githubstatus.com' })]
      await mountView({ fetchImpl: makeFetch({ sources, handlers: importHandlers({ '^POST .*/import$': () => json({ created: [], skipped: [{ external_component_id: 9, reason: 'vanished' }] }) }) }) })
      await chooseSource()
      await importCheckbox('Solo').setValue(true)
      await submitImport()
      expect(wrapper.find('.ext-import__result').exists()).toBe(true)
      await fieldByLabel('Externe Quelle').setValue('2')
      await flushPromises()
      expect(wrapper.find('.ext-import__result').exists()).toBe(false)
      expect(wrapper.find('.ext-import__count').text()).toBe('0 gewählt')
    })

    it('behandelt eine leere Antwort und unbekannte übersprungene IDs ohne Fehler', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ '^POST .*/import$': () => json(null) }) }) })
      await chooseSource()
      await importCheckbox('Solo').setValue(true)
      await submitImport()
      expect(wrapper.find('.alert-success').text()).toContain('0 angelegt, 0 übersprungen')
      wrapper.unmount()
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ '^POST .*/import$': () => json({ skipped: [{ external_component_id: 555, reason: 'vanished' }] }) }) }) })
      await chooseSource()
      await importCheckbox('Solo').setValue(true)
      await submitImport()
      expect(wrapper.find('.ext-import__result').text()).toContain('555: verschwunden')
    })

    it('leert die Quellenwahl der Übernahme, wenn diese Quelle gelöscht wird', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers() }) })
      await chooseSource()
      stubConfirm(true)
      await menuItem('Löschen').trigger('click')
      await flushPromises()
      expect(fieldByLabel('Externe Quelle').element.value).toBe('')
      expect(wrapper.find('.ext-import__list').exists()).toBe(false)
      vi.unstubAllGlobals()
    })

    it('übersetzt Karte und Gründe ins Englische', async () => {
      i18n.global.locale.value = 'en'
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers({ '^POST .*/import$': () => json({ created: [], skipped: [{ external_component_id: 9, reason: 'empty_group' }] }) }) }) })
      await fieldByLabel('External source').setValue('1')
      await flushPromises()
      await importCheckbox('Solo').setValue(true)
      await submitImport()
      expect(wrapper.text()).toContain('Adopt components')
      expect(wrapper.find('.ext-import__result').text()).toContain('Solo: group without current components')
    })

    it('hat keine axe-Verstöße mit geöffneter Liste', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: importHandlers() }) })
      await chooseSource()
      expect(await axe(wrapper.element)).toHaveNoViolations()
    })
  })

  // Eine nie abgerufene oder inaktive Quelle hat keine Bausteine; das war ein
  // Ladefehler im Feld ("Die Liste konnte nicht geladen werden"), obwohl nur ein
  // Abruf fehlt.
  describe('Bausteinliste einer nie abgerufenen Quelle', () => {
    const fresh = (over = {}) => sourceFixture({ last_fetch_at: null, last_fetch_ok: null, last_success_at: null, component_count: 0, link_count: 0, ...over })

    it('sagt "erst abrufen" statt eines Ladefehlers, wenn die Liste scheitert', async () => {
      await mountView({ fetchImpl: makeFetch({ sources: [fresh()], handlers: { 'external-sources/1/components$': () => json({ error: 'x' }, 500) } }) })
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      expect(wrapper.text()).toContain('Diese Quelle hat noch keine Bausteine. Erst abrufen')
      expect(wrapper.text()).not.toContain('Die Liste konnte nicht geladen werden.')
    })

    it('sagt "erst abrufen", wenn die Liste leer ankommt, und "aktivieren" bei inaktiven Quellen', async () => {
      const empty = () => json({ groups: [], components: [] })
      await mountView({ fetchImpl: makeFetch({ sources: [fresh()], handlers: { 'external-sources/1/components$': empty } }) })
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      expect(wrapper.text()).toContain('Erst abrufen (Aktionen › Jetzt abrufen)')
      wrapper.unmount()
      await mountView({ fetchImpl: makeFetch({ sources: [fresh({ active: false })], handlers: { 'external-sources/1/components$': empty } }) })
      await fieldByLabel('Quelle').setValue('1')
      await flushPromises()
      expect(wrapper.text()).toContain('Diese Quelle ist inaktiv und hat noch keine Bausteine. Erst aktivieren')
    })

    it('zeigt auch in der Übernahme-Karte den Hinweis statt eines Fehlers', async () => {
      await mountView({ fetchImpl: makeFetch({ sources: [fresh()], handlers: { 'external-sources/1/components$': () => json({ error: 'x' }, 500) } }) })
      await fieldByLabel('Externe Quelle').setValue('1')
      await flushPromises()
      expect(wrapper.find('.ext-import__hint').text()).toContain('Erst abrufen')
    })

    it('meldet einen echten Ladefehler einer bereits abgerufenen Quelle weiterhin', async () => {
      await mountView({ fetchImpl: makeFetch({ handlers: { 'external-sources/1/components$': () => json({ error: 'x' }, 500) } }) })
      await fieldByLabel('Externe Quelle').setValue('1')
      await flushPromises()
      expect(wrapper.find('.ext-import__hint').text()).toContain('Die Liste konnte nicht geladen werden.')
    })
  })

  describe('Rechte (external_sources:read)', () => {
    it('zeigt Daten, aber keine Schreibknöpfe', async () => {
      await mountView({ perms: 'external_sources:read' })
      expect(wrapper.text()).toContain('Example Cloud')
      expect(buttonByText('Neue Quelle')).toBeUndefined()
      // Leser können die Quelle ansehen, aber weder abrufen noch löschen.
      expect(wrapper.findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Ansehen'])
      expect(wrapper.find('.ext-link-form').exists()).toBe(false)
      expect(buttonByText('Lösen')).toBeUndefined()
    })
  })

  it('übersetzt die Seite ins Englische', async () => {
    i18n.global.locale.value = 'en'
    await mountView()
    expect(wrapper.text()).toContain('External sources')
    expect(wrapper.find('tbody tr').text()).toContain('Reachable')
    expect(wrapper.find('tbody tr').text()).toContain('2 minutes ago')
  })

  it('hat keine axe-Verstöße', async () => {
    await mountView()
    expect(await axe(wrapper.element)).toHaveNoViolations()
  })
})

// Der Koppeln-Knopf saß am unteren Rand der Zeile und rutschte unter die
// Eingabefelder, sobald ein Feld einen Hinweistext zeigte.
describe('AdminExternalSources Koppeln-Zeile (Quelltext)', () => {
  const src = readFileSync(resolve(process.cwd(), 'src/views/admin/AdminExternalSources.vue'), 'utf-8')
  const css = (sel) => src.match(new RegExp(`\\${sel} \\{([^}]*)\\}`))?.[1] ?? ''
  it('richtet die Formularzeile oben aus statt am Zeilenende', () => {
    expect(css('.ext-link-form')).toMatch(/align-items:\s*start/)
    expect(css('.ext-link-form')).not.toMatch(/align-items:\s*end/)
  })
  it('setzt den Knopf per unsichtbarer Beschriftung auf die Höhe der Felder', () => {
    expect(src).toMatch(/ext-link-form__spacer[^>]*aria-hidden="true"/)
  })

  // Ohne Änderungsrecht öffnet „Ansehen“ das Formular gesperrt, ohne Speichern.
  it('öffnet das Formular ohne external_sources:update gesperrt', async () => {
    await mountView({ perms: 'external_sources:read' })
    await menuItem('Ansehen').trigger('click')
    await flushPromises()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.form-lock-hint').exists()).toBe(true)
    expect(wrapper.find('form.edit-form button[type="submit"]').exists()).toBe(false)
  })

  it('bietet „Jetzt abrufen“ nur mit external_sources:update an, Löschen nur mit external_sources:delete', async () => {
    await mountView({ perms: 'external_sources:update' })
    expect(['Jetzt abrufen', 'Bearbeiten'].every(t => menuItem(t))).toBe(true)
    expect(menuItem('Löschen')).toBeUndefined()
    wrapper.unmount()
    await mountView({ perms: 'external_sources:delete' })
    expect(menuItem('Löschen')).toBeDefined()
    expect(menuItem('Jetzt abrufen')).toBeUndefined()
    expect(menuItem('Bearbeiten')).toBeUndefined()
  })
})
