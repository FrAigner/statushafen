import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import AdminOverview from './AdminOverview.vue'
import { axe } from '../../test-support/axe.js'
import { expectNoInlineStyles } from '../../test-support/noInlineStyles.js'

function overviewFixture() {
  return {
    total_pages: 2,
    total_open_incidents: 3,
    total_subscribers: 1500,
    worst_status: 'partial_outage',
    pages: [
      { id: 1, name: 'Acme Corp', slug: 'acme', status: 'partial_outage', components: 5, subscribers: 1200, open_incidents: 2 },
      { id: 2, name: 'Beta', slug: 'beta', status: 'operational', components: 3, subscribers: 300, open_incidents: 0 },
    ],
  }
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: AdminOverview },
      { path: '/admin/incidents', component: { template: '<div/>' } },
    ],
  })
  router.push('/')
  return router
}

let activeWrapper = null

async function mountOverview(fetchImpl) {
  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse(overviewFixture()))
  const router = makeRouter()
  await router.isReady()

  const wrapper = mount(AdminOverview, { global: { plugins: [i18n, router] } })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper, router }
}

// KPI-Karten als { Beschriftung: Wert } (ohne den Screenreader-Zusatz).
const kpis = (wrapper) => Object.fromEntries(wrapper.findAll('.kpi').map(k => [
  k.find('.kpi__label').text(),
  k.find('.kpi__value').text().replace('Handlungsbedarf', '').trim(),
]))

describe('AdminOverview', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('zeigt einen Ladezustand, bevor die Daten eintreffen', () => {
    global.fetch = vi.fn(() => new Promise(() => {}))
    const router = makeRouter()
    const wrapper = mount(AdminOverview, { global: { plugins: [i18n, router] } })
    activeWrapper = wrapper

    expect(wrapper.find('[role="status"][aria-busy="true"]').text()).toContain('Lädt')
    expect(wrapper.find('.empty-state').exists()).toBe(false)
  })

  it('Seitentitel ist eine h2 (PageHeader level 2), keine h1', async () => {
    const { wrapper } = await mountOverview()

    expect(wrapper.findAll('h2.page-head__title').map(h => h.text())).toEqual(['Mandanten-Übersicht'])
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('.page-head__subtitle').text()).toBe('Aggregierter Status aller Statusseiten')
  })

  it('rendert KPIs aus der API-Antwort', async () => {
    const { wrapper } = await mountOverview()

    expect(kpis(wrapper)).toEqual({
      'Aktive Statusseiten': '2',
      'Offene Vorfälle': '3',
      'Abonnenten gesamt': '1,5k',
      Gesamtstatus: 'Teilausfall',
    })
  })

  it('rendert eine Tabellenzeile je Statusseite mit Initialen und Link zu den Incidents', async () => {
    const { wrapper } = await mountOverview()

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('Acme Corp')
    expect(rows[0].text()).toContain('/acme')
    expect(rows[0].find('.avatar').text()).toBe('AC')
    expect(rows[0].find('a').attributes('href')).toBe('/admin/incidents?page=1')
    expect(rows[0].find('td[data-label="Komponenten"]').text()).toBe('5')
  })

  it('nutzt den gemeinsamen Tabellenstil mit rechtsbündigen Zahlenspalten', async () => {
    const { wrapper } = await mountOverview()

    expect(wrapper.find('.data-table-wrapper table.data-table').exists()).toBe(true)
    const numericHeads = wrapper.findAll('th.is-numeric').map(th => th.text())
    expect(numericHeads).toEqual(['Komponenten', 'Abonnenten', 'Offene Vorfälle'])
  })

  it('zeigt den Empty-State ohne Statusseiten', async () => {
    const { wrapper } = await mountOverview(vi.fn().mockResolvedValue(jsonResponse({
      total_pages: 0, total_open_incidents: 0, total_subscribers: 0, worst_status: 'operational', pages: [],
    })))

    expect(wrapper.find('.empty-state__title').text()).toBe('Keine Statusseiten vorhanden.')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('zeigt 0 für fehlende Summen', async () => {
    const { wrapper } = await mountOverview(vi.fn().mockResolvedValue(jsonResponse({ pages: [] })))

    const values = kpis(wrapper)
    expect(values['Aktive Statusseiten']).toBe('0')
    expect(values['Offene Vorfälle']).toBe('0')
    expect(values['Abonnenten gesamt']).toBe('0')
  })

  it('färbt nur „Offene Vorfälle“ > 0 rot und sagt es per Text an (nicht nur per Farbe)', async () => {
    const { wrapper } = await mountOverview()

    const cards = wrapper.findAll('.kpi')
    expect(cards.map(c => c.classes('kpi--alert'))).toEqual([false, true, false, false])
    expect(cards[1].find('.kpi__sr').text()).toBe('Handlungsbedarf')
  })

  it('färbt „Offene Vorfälle“ bei 0 nicht rot', async () => {
    const fixture = overviewFixture()
    fixture.total_open_incidents = 0
    const { wrapper } = await mountOverview(vi.fn().mockResolvedValue(jsonResponse(fixture)))

    expect(wrapper.find('.kpi--alert').exists()).toBe(false)
    expect(wrapper.find('.kpi__sr').exists()).toBe(false)
  })

  it('hebt offene Incidents je Zeile nur bei Werten > 0 hervor', async () => {
    const { wrapper } = await mountOverview()

    const rows = wrapper.findAll('tbody tr')
    const cell = (row) => row.find('td[data-label="Offene Vorfälle"] span')
    expect(cell(rows[0]).classes()).toContain('open-incidents')
    expect(cell(rows[1]).classes()).not.toContain('open-incidents')
  })

  it.each([
    ['operational', 'Betriebsbereit', 'st-badge--operational'],
    ['degraded_performance', 'Beeinträchtigt', 'st-badge--degraded'],
    ['partial_outage', 'Teilausfall', 'st-badge--partial'],
    ['major_outage', 'Großstörung', 'st-badge--major'],
  ])('zeigt %s als „%s“ mit StatusBadge (%s)', async (status, label, cls) => {
    const fixture = overviewFixture()
    fixture.worst_status = status
    fixture.pages[0].status = status
    const { wrapper } = await mountOverview(vi.fn().mockResolvedValue(jsonResponse(fixture)))

    expect(kpis(wrapper).Gesamtstatus).toBe(label)
    const badge = wrapper.findAll('tbody tr')[0].find('.st-badge')
    expect(badge.text()).toBe(label)
    expect(badge.classes()).toContain(cls)
  })

  it('behandelt einen unbekannten Gesamtstatus wie „Betriebsbereit“', async () => {
    const fixture = overviewFixture()
    fixture.worst_status = 'seltsam'
    const { wrapper } = await mountOverview(vi.fn().mockResolvedValue(jsonResponse(fixture)))

    expect(kpis(wrapper).Gesamtstatus).toBe('Betriebsbereit')
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountOverview(vi.fn().mockResolvedValue(jsonResponse({ error: 'kaputt' }, 500)))

    expect(wrapper.find('.alert-error').text()).toContain('kaputt')
  })

  // Regression (Stufe 4b): Die View trug Inline-Styles und einen
  // festen Hex-Wert (#6b4de6) für die Abonnenten-Kennzahl.
  it('hat keine Inline-Styles und keine Hex-Farben im Quelltext', async () => {
    const { wrapper } = await mountOverview()
    expect(wrapper.findAll('[style]')).toHaveLength(0)

    expectNoInlineStyles(wrapper, import.meta.url, 'AdminOverview.vue')
  })

  it('hat keine axe-Funde', async () => {
    const { wrapper } = await mountOverview()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde im Leerzustand', async () => {
    const { wrapper } = await mountOverview(vi.fn().mockResolvedValue(jsonResponse({ pages: [] })))
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Richtung C: Tabelle in einer Karte mit Suche im Kopf.
  it('zeigt die Tabelle in der Karte „Alle Statusseiten“ und sucht nach Name und Kurzname', async () => {
    const { wrapper } = await mountOverview()
    expect(wrapper.find('section.comp-card h3').text()).toBe('Alle Statusseiten')

    const search = wrapper.find('input[type="search"]')
    await search.setValue('beta')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody').text()).toContain('Beta')
    expect(wrapper.find('[role="status"].sr-only').text()).toBe('1 Treffer')

    await search.setValue('ACME')
    expect(wrapper.find('tbody').text()).toContain('Acme Corp')

    await search.setValue('gibtesnicht')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state__title').text()).toBe('Keine Statusseite gefunden.')
  })
})
