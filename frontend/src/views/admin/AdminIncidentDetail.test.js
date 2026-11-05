import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminIncidentDetail from './AdminIncidentDetail.vue'
import { axe } from '../../test-support/axe.js'
import { fieldByLabel } from '../../test-support/fieldByLabel.js'
import { expectNoInlineStyles } from '../../test-support/noInlineStyles.js'

function incidentFixture(overrides = {}) {
  return {
    ID: 1, Title: 'API down', Status: 'investigating', CreatedAt: '2026-07-01T10:00:00Z',
    Components: [], Tags: [],
    updates: [{ id: 1, status: 'investigating', message: 'Untersuchung läuft', created_at: '2026-07-01T10:05:00Z' }],
    post_mortem: { summary: 'Zusammenfassung', published_at: '2026-07-02T09:00:00Z', actions: [] },
    ...overrides,
  }
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/admin/incidents/:pageId/:id', component: AdminIncidentDetail },
      { path: '/admin/incidents', component: { template: '<div/>' } },
    ],
  })
}

let activeWrapper = null

// Die Ansicht lädt Sprachkatalog und Seitensprachen (Sprachumschalter der Updates).
// Der Wrapper beantwortet beide Abrufe und reicht alles andere an die Test-Attrappe durch,
// deren Aufrufzahlen so unverändert bleiben. Standard: nur die Standardsprache (kein Umschalter).
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }, { Code: 'fr', Name: 'Français' }]
const onlyDefault = [{ LanguageCode: 'de', IsDefault: true }]
function withLanguages(base, languages = onlyDefault) {
  return vi.fn((url, o) => {
    if (url === '/api/languages') return Promise.resolve(jsonResponse(catalog))
    if (/\/api\/pages\/\d+\/languages$/.test(url)) return Promise.resolve(jsonResponse(languages))
    return base(url, o)
  })
}

async function mountDetail({ fetchImpl, languages } = {}) {
  const router = makeRouter()
  router.push('/admin/incidents/1/1')
  await router.isReady()

  global.fetch = withLanguages(fetchImpl || vi.fn().mockResolvedValue(jsonResponse(incidentFixture())), languages)

  const wrapper = mount(AdminIncidentDetail, {
    global: { plugins: [i18n, router], stubs: onyxFormStubs },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper, router }
}

describe('AdminIncidentDetail', () => {
  beforeEach(() => {
    useAuth().logout()
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

  it('lädt den Incident und zeigt Titel, Melde-Zeitstempel und Verlauf im deutschen Format', async () => {
    const { wrapper } = await mountDetail()

    expect(wrapper.find('h2.page-head__title').text()).toBe('API down')
    const expectedReported = new Date('2026-07-01T10:00:00Z').toLocaleString('de-DE', { dateStyle: 'medium', timeStyle: 'short' })
    expect(wrapper.find('.m-v.mono').text()).toBe(expectedReported)

    const rows = wrapper.findAll('.timeline > li')
    expect(rows).toHaveLength(1)
    const expectedUpdateTime = new Date('2026-07-01T10:05:00Z').toLocaleString('de-DE', { dateStyle: 'medium', timeStyle: 'short' })
    expect(rows[0].find('.tl-time').text()).toBe(expectedUpdateTime)
  })

  it('zeigt das Post-Mortem-Veröffentlichungsdatum im deutschen Langformat', async () => {
    const { wrapper } = await mountDetail()

    const expectedDay = new Date('2026-07-02T09:00:00Z').toLocaleDateString('de-DE', { dateStyle: 'long' })
    expect(wrapper.find('.pm-date').text()).toBe(expectedDay)
  })

  it('formatiert Zeitstempel im englischen Format, wenn die UI-Sprache Englisch ist', async () => {
    i18n.global.locale.value = 'en'
    const { wrapper } = await mountDetail()

    const expectedReported = new Date('2026-07-01T10:00:00Z').toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' })
    expect(wrapper.find('.m-v.mono').text()).toBe(expectedReported)

    const expectedDay = new Date('2026-07-02T09:00:00Z').toLocaleDateString('en-US', { dateStyle: 'long' })
    expect(wrapper.find('.pm-date').text()).toBe(expectedDay)
  })

  it('springt bei #update zum Update-Formular und fokussiert den Status', async () => {
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: 'incidents:create,incidents:read' })
    global.fetch = withLanguages(vi.fn().mockResolvedValue(jsonResponse(incidentFixture())))
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    const router = makeRouter()
    router.push('/admin/incidents/1/1#update')
    await router.isReady()
    const wrapper = mount(AdminIncidentDetail, { global: { plugins: [i18n, router], stubs: onyxFormStubs }, attachTo: document.body })
    activeWrapper = wrapper
    await flushPromises()
    expect(scroll).toHaveBeenCalled()
    expect(document.activeElement).toBe(wrapper.find('#update-form select').element)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}, 500))
    const { wrapper } = await mountDetail({ fetchImpl: fetchMock })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  it('hat keine axe-Funde mit Update- und Post-Mortem-Formular samt Maßnahmenzeile', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const { wrapper } = await mountDetail()
    await wrapper.find('.action-add').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde in der reinen Leseansicht', async () => {
    const { wrapper } = await mountDetail()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
  it('Seitentitel ist eine h2 (PageHeader level 2, die Topbar trägt die h1) mit Brotkrumen zur Liste', async () => {
    const { wrapper } = await mountDetail()

    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.findAll('h2.page-head__title')).toHaveLength(1)
    const crumbs = wrapper.findAll('.page-head__crumbs li')
    expect(crumbs.map(c => c.text())).toEqual(['Vorfälle', '#1'])
    expect(crumbs[0].find('a').attributes('href')).toBe('/admin/incidents')
    expect(crumbs[1].find('[aria-current="page"]').exists()).toBe(true)
  })

  it('zeigt vor dem Laden und im Fehlerfall den allgemeinen Titel „Vorfall“ samt Brotkrumen-Nummer aus der Route', async () => {
    global.fetch = vi.fn(() => new Promise(() => {}))
    const router = makeRouter()
    router.push('/admin/incidents/1/7')
    await router.isReady()
    const wrapper = mount(AdminIncidentDetail, { global: { plugins: [i18n, router], stubs: onyxFormStubs } })
    activeWrapper = wrapper

    expect(wrapper.find('h2.page-head__title').text()).toBe('Vorfall')
    expect(wrapper.findAll('.page-head__crumbs li').map(c => c.text())).toEqual(['Vorfälle', '#7'])
    expect(wrapper.find('[role="status"][aria-busy="true"]').exists()).toBe(true)
  })

  it('zeigt den Status im Kopf und im Verlauf als StatusBadge (neueste Updates zuerst)', async () => {
    const fixture = incidentFixture({
      Status: 'resolved',
      updates: [
        { id: 1, status: 'investigating', message: 'Erste Meldung', created_at: '2026-07-01T10:05:00Z' },
        { id: 2, status: 'resolved', message: 'Behoben', created_at: '2026-07-01T11:00:00Z', author_email: 'a@x.de' },
      ],
    })
    const { wrapper } = await mountDetail({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(fixture)) })

    const head = wrapper.find('.head .st-badge')
    expect(head.text()).toBe('Behoben')
    expect(head.classes()).toContain('st-badge--operational')
    expect(head.classes()).toContain('st-badge--md')

    const rows = wrapper.findAll('.timeline > li')
    expect(rows.map(r => r.find('.st-badge').text())).toEqual(['Behoben', 'Untersuchung'])
    expect(rows[0].find('.tl-author').text()).toBe('— a@x.de')
    expect(rows[1].find('.tl-author').exists()).toBe(false)
    expect(wrapper.find('.ent-pill').exists()).toBe(false)
  })

  it('zeigt „Noch keine Updates.“ statt einer leeren Zeitleiste', async () => {
    const { wrapper } = await mountDetail({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(incidentFixture({ updates: [], Components: [{ ID: 1, Name: 'API' }], Tags: [{ ID: 2, Name: 'DE' }] }))) })

    expect(wrapper.text()).toContain('Noch keine Updates.')
    expect(wrapper.find('.timeline').exists()).toBe(false)
    expect(wrapper.find('dl').text()).toContain('API')
    expect(wrapper.find('dl').text()).toContain('DE')
  })

  it('zeigt ohne incidents:write weder Update- noch Post-Mortem-Formular', async () => {
    const { wrapper } = await mountDetail()

    expect(wrapper.find('.update-form').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Post-Mortem bearbeiten')
  })

  it('zeigt Post-Mortem als Entwurf (neutral) bzw. veröffentlicht und die Folgemaßnahmen mit StatusBadge', async () => {
    const draft = incidentFixture({
      post_mortem: {
        summary: 'S', root_cause: 'Ursache', impact: 'Wirkung', resolution: 'Fix', detection: 'Alarm',
        published_at: null,
        actions: [
          { id: 1, text: 'A1', owner: 'ich', state: 'done' },
          { id: 2, text: 'A2', owner: 'du', state: 'in_progress' },
          { id: 3, text: 'A3', owner: 'er', state: 'open' },
        ],
      },
    })
    const { wrapper } = await mountDetail({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(draft)) })

    const state = wrapper.find('.pm-state')
    expect(state.text()).toBe('Entwurf')
    expect(state.classes()).toContain('st-badge--unknown')
    expect(wrapper.find('.pm-date').exists()).toBe(false)
    expect(wrapper.findAll('.pm-grid h4').map(h => h.text())).toEqual(['Grundursache', 'Auswirkung', 'Behebung', 'Erkennung'])
    const actions = wrapper.findAll('.actions .st-badge')
    expect(actions.map(a => a.text())).toEqual(['Erledigt', 'In Arbeit', 'Geplant'])
    expect(actions[0].classes()).toContain('st-badge--operational')
    expect(actions[2].classes()).toContain('st-badge--unknown')

    const published = await mountDetail()
    expect(published.wrapper.find('.pm-state').text()).toBe('Veröffentlicht')
    expect(published.wrapper.find('.pm-state').classes()).toContain('st-badge--operational')
  })

  // Update-Nachricht ist der Editor; das Post-Mortem bleibt Klartext.
  it('Update-Nachricht ist der RichTextEditor, die Post-Mortem-Felder nicht', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const { wrapper } = await mountDetail({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse(incidentFixture())) })
    expect(fieldByLabel(wrapper, 'Update-Nachricht').element.closest('.rte')).not.toBeNull()
    expect(fieldByLabel(wrapper, 'Zusammenfassung').element.closest('.rte')).toBeNull()
  })

  it('sendet ein Update mit Rohwert-Status und lädt neu', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(incidentFixture()))
    const { wrapper } = await mountDetail({ fetchImpl: fetchMock })

    const select = fieldByLabel(wrapper, 'Neuer Status')
    expect(select.element.value).toBe('investigating')
    expect(select.findAll('option').map(o => o.text())).toEqual(['Untersuchung', 'Identifiziert', 'Beobachtung', 'Behoben'])
    await select.setValue('identified')
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Ursache gefunden')
    await wrapper.find('.update-form form').trigger('submit')
    await flushPromises()

    const post = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(post[0]).toBe('/api/pages/1/incidents/1/updates')
    expect(JSON.parse(post[1].body)).toEqual({ status: 'identified', message: 'Ursache gefunden' })
    expect(fetchMock.mock.calls.filter(([, o]) => !o?.method).length).toBe(2)
  })

  it('zeigt einen Fehler, wenn das Update fehlschlägt', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const fetchMock = vi.fn(async (url, o = {}) => (o.method === 'POST' ? jsonResponse({ error: 'kaputt' }, 500) : jsonResponse(incidentFixture())))
    const { wrapper } = await mountDetail({ fetchImpl: fetchMock })
    await wrapper.find('.update-form form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.update-form .alert-error').text()).toContain('Update fehlgeschlagen')
  })

  it('bearbeitet das Post-Mortem mit Maßnahmenzeilen und speichert Entwurf bzw. Veröffentlichung', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(incidentFixture()))
    const { wrapper } = await mountDetail({ fetchImpl: fetchMock })

    expect(fieldByLabel(wrapper, 'Zusammenfassung').element.value).toBe('Zusammenfassung')
    await fieldByLabel(wrapper, 'Grundursache').setValue('Konfig')
    await wrapper.find('.action-add').trigger('click')
    const row = wrapper.find('.action-row.edit')
    await row.find('input[aria-label="Maßnahme"]').setValue('Alarm ergänzen')
    await row.find('input[aria-label="Verantwortliche Person der Maßnahme"]').setValue('team')
    await row.find('select').setValue('done')

    const save = (label) => wrapper.findAll('button').find(b => b.text() === label)
    await save('Als Entwurf speichern').trigger('click')
    await flushPromises()
    let put = fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT').at(-1)
    expect(put[0]).toBe('/api/pages/1/incidents/1/postmortem')
    expect(JSON.parse(put[1].body)).toMatchObject({
      root_cause: 'Konfig', publish: false,
      actions: [{ text: 'Alarm ergänzen', owner: 'team', state: 'done' }],
    })

    await save('Veröffentlichen').trigger('click')
    await flushPromises()
    put = fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT').at(-1)
    expect(JSON.parse(put[1].body).publish).toBe(true)
  })

  it('sendet neue Maßnahmenzeilen mit und entfernt sie wieder', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    // Antwort bleibt unverändert, daher setzt jedes Neuladen die Zeilen zurück; Zeilen vor dem Speichern prüfen.
    const { wrapper } = await mountDetail()
    await wrapper.find('.action-add').trigger('click')
    await wrapper.find('.action-add').trigger('click')
    expect(wrapper.findAll('.action-row.edit')).toHaveLength(2)

    await wrapper.find('.action-remove').trigger('click')
    expect(wrapper.findAll('.action-row.edit')).toHaveLength(1)
    expect(wrapper.find('select[aria-label="Status der Maßnahme"]').findAll('option').map(o => o.text())).toEqual(['Geplant', 'In Arbeit', 'Erledigt'])
  })

  it('zeigt einen Fehler, wenn das Speichern des Post-Mortems fehlschlägt', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const fetchMock = vi.fn(async (url, o = {}) => (o.method === 'PUT' ? jsonResponse({ error: 'kaputt' }, 500) : jsonResponse(incidentFixture())))
    const { wrapper } = await mountDetail({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text() === 'Veröffentlichen').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Speichern fehlgeschlagen')
  })

  it('hat keine Inline-Styles und keine Hex-Farben', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const { wrapper } = await mountDetail()
    await wrapper.find('.action-add').trigger('click')
    expectNoInlineStyles(wrapper, import.meta.url, 'AdminIncidentDetail.vue')
  })
  it('zeigt „—“ bei fehlenden Komponenten und Tags und übernimmt die Zusammenfassung ins Speichern', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(incidentFixture({ Components: null, Tags: null })))
    const { wrapper } = await mountDetail({ fetchImpl: fetchMock })

    expect(wrapper.findAll('dd').slice(0, 2).map(d => d.text())).toEqual(['—', '—'])
    await fieldByLabel(wrapper, 'Zusammenfassung').setValue('Neu')
    await wrapper.findAll('button').find(b => b.text() === 'Als Entwurf speichern').trigger('click')
    await flushPromises()
    const put = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(JSON.parse(put[1].body).summary).toBe('Neu')
  })

  it('zeigt Seitenname in den Brotkrumen und die Meldezeit relativ im Untertitel', async () => {
    useTenant().setActiveTenant('1')
    useTenant().setMyPages([{ ID: 1, Name: 'Meine Seite' }])
    const { wrapper } = await mountDetail()

    const crumbs = wrapper.findAll('.page-head__crumbs li')
    expect(crumbs.map(c => c.text())).toEqual(['Meine Seite', 'Vorfälle', '#1'])
    expect(wrapper.find('.page-head__subtitle').text()).toMatch(/^Gemeldet vor /)
    useTenant().setMyPages([])
  })
})

// Folgemaßnahmen im Post-Mortem-Formular: Eingaben und Auswahl wie die übrigen Felder
// (Schriftgröße, Höhe, eigener Pfeil), Platz für den Platzhalter „Verantwortlich“.
describe('AdminIncidentDetail: Zeile der Folgemaßnahmen (Layout)', () => {
  const sfc = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'AdminIncidentDetail.vue'), 'utf8')
  // Alle Blöcke, deren Selektorliste auf `selector` endet, zusammengefasst.
  const rule = (selector) => [...sfc.matchAll(/([^{}]+)\{([^}]*)\}/g)]
    .filter(m => m[1].trim().endsWith(selector))
    .map(m => m[2]).join(';')

  it('setzt Eingaben und Auswahl auf eine kleinere Schrift und die Steuerhöhe', () => {
    const r = rule('.action-row.edit .form-select')
    expect(r).toMatch(/font-size:\s*var\(--fs-sm\)/)
    expect(r).toMatch(/height:\s*var\(--ctl-h\)/)
  })

  it('gibt der Auswahl einen eigenen Pfeil statt des Browser-Standards', () => {
    const r = rule('.action-row.edit .form-select')
    expect(r).toMatch(/appearance:\s*none/)
    expect(r).toMatch(/background-image:[^;]*linear-gradient[^;]*currentColor/)
  })

  it('lässt dem Feld „Verantwortlich“ genug Breite für den Platzhalter', () => {
    expect(Number(rule('.w-owner').match(/width:\s*(\d+)px/)?.[1])).toBeGreaterThanOrEqual(180)
  })
  // „Als Entwurf speichern“ lieferte PUT 200 ohne Meldung, und die Seite sprang nach oben,
  // weil load() den Inhalt durch den Ladezustand ersetzte.
  it('meldet das Speichern des Post-Mortems und tauscht die Seite dabei nicht aus', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'incidents:write' })
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(incidentFixture()))
    const { wrapper } = await mountDetail({ fetchImpl: fetchMock })
    const form = wrapper.find('form, .edit-card, .pm-edit').element
    const save = (label) => wrapper.findAll('button').find(b => b.text() === label)

    await save('Als Entwurf speichern').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-success').text()).toContain('Post-Mortem als Entwurf gespeichert.')
    expect(wrapper.find('form, .edit-card, .pm-edit').element).toBe(form)

    await save('Veröffentlichen').trigger('click')
    await flushPromises()
    expect(wrapper.find('.alert-success').text()).toContain('Post-Mortem veröffentlicht.')
    await wrapper.find('.alert-success .alert-close').trigger('click')
    expect(wrapper.find('.alert-success').exists()).toBe(false)
  })
})
