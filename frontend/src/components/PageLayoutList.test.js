import { describe, it, expect, afterEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import i18n from '../i18n'
import PageLayoutList from './PageLayoutList.vue'
import StatusBadge from './StatusBadge.vue'
import AvailabilityBars from './AvailabilityBars.vue'
import { axe } from '../test-support/axe.js'

enableAutoUnmount(afterEach)

const today = new Date().toISOString()
const tree = [
  { ID: 1, Name: 'Web', Status: 'degraded_performance', Children: [{ ID: 2, Name: 'CDN', Status: 'operational', Children: [] }] },
  { ID: 3, Name: 'API', Status: 'weird_status' },
]
const incidents = [
  { ID: 10, Title: 'CDN langsam', Message: 'm1', Status: 'investigating', IsMaintenance: false, Components: [{ ID: 2, Name: 'CDN' }], CreatedAt: today, UpdatedAt: today },
  { ID: 11, Title: 'Alt', Message: 'm2', Status: 'in_progress', IsMaintenance: true, Components: [], CreatedAt: '2020-01-01T10:00:00Z' },
  { ID: 12, Title: 'Komisch', Message: 'm3', Status: 'other', Components: [], CreatedAt: '2020-01-01T09:00:00Z' },
]

const days = (n = 90) => Array.from({ length: n }, (_, i) => ({
  date: new Date(Date.UTC(2026, 6, 13 + i)).toISOString().slice(0, 10),
  status: i === n - 1 ? 'degraded_performance' : 'operational',
  availability: i === n - 1 ? 97.5 : 100,
  downtime_seconds: 0,
}))
// Der Endpunkt liefert nur Blätter (Gruppen haben keine Zeile), hier CDN und API.
const availability = {
  components: [
    { id: 2, name: 'CDN', uptime_percent: 99.9, days: days() },
    { id: 3, name: 'API', uptime_percent: null, days: days().map(d => ({ ...d, status: 'no_data', availability: null })) },
  ],
}

const mountList = (props = {}) => mount(PageLayoutList, {
  props: { components: tree, timelineIncidents: incidents, availability, ...props },
  global: { plugins: [i18n] },
})

describe('PageLayoutList (Redesign 4a)', () => {
  it('zeigt keinen Gesamtstatus mehr, der steht oben in StatusPage', () => {
    const w = mountList()
    expect(w.find('.pl-overall').exists()).toBe(false)
    expect(w.find('[role="status"]').exists()).toBe(false)
  })

  it('übersetzt jeden Vorfallstatus in der Zeitleiste mit StatusBadge', () => {
    i18n.global.locale.value = 'de'
    const mk = (ID, Status) => ({ ID, Title: `V${ID}`, Message: 'm', Status, Components: [], CreatedAt: today })
    const list = [mk(20, 'investigating'), mk(21, 'identified'), mk(22, 'monitoring'), mk(23, 'resolved'), mk(24, 'scheduled')]
    const w = mountList({ timelineIncidents: list })
    const badges = w.findAll('.pl-incident').map(a => a.findComponent(StatusBadge))
    expect(badges.map(b => b.text())).toEqual(['Untersuchung', 'Identifiziert', 'Beobachtung', 'Behoben', 'Geplant'])
    expect(badges.map(b => b.props('status'))).toEqual(['major', 'partial', 'degraded', 'operational', 'maintenance'])
  })

  it('rendert je obersten Knoten eine Karte mit Zeilen für Kinder, Einrückung per Klasse', () => {
    const w = mountList()
    expect(w.findAll('.pl-card')).toHaveLength(2)
    expect(w.findAll('.pl-row')).toHaveLength(3)
    expect(w.findAll('.pl-row')[0].classes()).toContain('pl-d0')
    expect(w.findAll('.pl-row')[1].classes()).toContain('pl-d1')
    expect(w.html()).not.toContain('style=')
  })

  it('deckelt die Einrückungstiefe', () => {
    let node = { ID: 99, Name: 'Blatt', Status: 'operational', Children: [] }
    for (let id = 98; id >= 90; id--) node = { ID: id, Name: `E${id}`, Status: 'operational', Children: [node] }
    const rows = mountList({ components: [node], availability: null }).findAll('.pl-row')
    expect(rows).toHaveLength(10)
    expect(rows.at(-1).classes()).toContain('pl-d5')
    expect(rows.at(-1).classes()).not.toContain('pl-d9')
  })

  it('beschriftet jeden Komponentenstatus mit StatusBadge (Form + Farbe + Text), Unbekanntes neutral', () => {
    i18n.global.locale.value = 'de'
    const nodes = ['operational', 'degraded_performance', 'partial_outage', 'major_outage'].map((Status, n) => ({ ID: n + 1, Name: `K${n}`, Status }))
    const badges = mountList({ components: nodes, availability: null }).findAll('.pl-row').map(r => r.findComponent(StatusBadge))
    expect(badges.map(b => b.text())).toEqual(['Betriebsbereit', 'Beeinträchtigt', 'Teilausfall', 'Großstörung'])
    const w = mountList()
    expect(w.findAll('.pl-row')[2].findComponent(StatusBadge).text()).toBe('weird_status')
    expect(w.find('.pl-chip').exists()).toBe(false)
  })

  it('zeigt Verfügbarkeitsbalken nur für Komponenten mit Daten, nicht für Gruppen', () => {
    const w = mountList()
    const rows = w.findAll('.pl-row')
    expect(rows[0].findComponent(AvailabilityBars).exists()).toBe(false) // Gruppe „Web“
    const cdn = rows[1].findComponent(AvailabilityBars)
    expect(cdn.exists()).toBe(true)
    expect(cdn.props('name')).toBe('CDN')
    expect(cdn.props('uptimePercent')).toBe(99.9)
    expect(cdn.findAll('.avail__day')).toHaveLength(90)
    expect(cdn.props('compact')).toBe(true)
  })

  it('zeigt bei uptime_percent null keine Prozentzahl und graue Tage für no_data', () => {
    const w = mountList()
    const api = w.findAll('.pl-row')[2]
    expect(api.find('.avail__percent').exists()).toBe(false)
    expect(api.findAll('.avail__day--none')).toHaveLength(90)
    expect(w.findAll('.pl-row')[1].find('.avail__percent').exists()).toBe(true)
  })

  it('lässt die Balken ohne Verfügbarkeitsdaten weg, der Rest bleibt', () => {
    const w = mountList({ availability: null })
    expect(w.findComponent(AvailabilityBars).exists()).toBe(false)
    expect(w.findAll('.pl-row')).toHaveLength(3)
    const ohneListe = mountList({ availability: {} })
    expect(ohneListe.findComponent(AvailabilityBars).exists()).toBe(false)
  })

  it('formatiert Tage der Zeitleiste lokalisiert', () => {
    const w = mountList()
    expect(w.findAll('.pl-day-mark').at(-1).text()).toBe('01.01.2020')
  })

  it('zeigt die Zeitleiste jüngste zuerst mit Tagesmarken und Statuslabels', () => {
    const w = mountList()
    expect(w.findAll('.pl-day-mark')).toHaveLength(2)
    expect(w.findAll('.pl-incident')[0].text()).toContain('CDN langsam')
    const badges = w.findAll('.pl-incident').map(a => a.findComponent(StatusBadge).text())
    expect(badges).toEqual(['Untersuchung', 'Wartung läuft', 'other'])
  })

  it('zeigt einen Leerzustand ohne Vorfälle', () => {
    const w = mountList({ timelineIncidents: [] })
    expect(w.find('.pl-timeline').exists()).toBe(false)
    expect(w.find('.pl-empty').text()).toBe('Keine Vorfälle.')
  })

  it('zeigt einen Leerzustand, wenn der Filter keine Komponente übrig lässt', () => {
    const w = mountList({ components: [] })
    expect(w.find('.empty-state').text()).toContain('Keine Komponenten gefunden.')
    expect(mountList().find('.empty-state').exists()).toBe(false)
  })

  it('meldet open-history mit dem Vorfall', async () => {
    const w = mountList()
    await w.find('.pl-link').trigger('click')
    expect(w.emitted('open-history')[0][0].ID).toBe(10)
  })

  it('nutzt die UI-Sprache für Texte', () => {
    i18n.global.locale.value = 'en'
    const w = mountList({ timelineIncidents: [] })
    expect(w.find('.pl-empty').text()).toBe('No incidents.')
    i18n.global.locale.value = 'de'
  })

  it('kennzeichnet gekoppelte externe Quellen je Zeile', async () => {
    i18n.global.locale.value = 'de'
    const nodes = [{
      ID: 1, Name: 'Compute', Status: 'major_outage', Children: [],
      external_sources: [{ external_component_id: 1, source_id: 2, source_name: 'Example Cloud', name: 'VM', status: 'major_outage', url: 'https://status.example.com', vanished: false }],
    }, { ID: 2, Name: 'Ohne', Status: 'operational', Children: [] }]
    const wrapper = mountList({ components: nodes, availability: null })
    expect(wrapper.findAll('.ext-list')).toHaveLength(1)
    expect(wrapper.find('.ext-list').text()).toContain('extern')
    expect(wrapper.find('.ext-list a').attributes('target')).toBe('_blank')
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde, auch nicht an den Verfügbarkeitsbalken', async () => {
    const wrapper = mountList()
    expect(wrapper.find('.avail__day').exists()).toBe(true)
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // externe Meldungen in der Zeitleiste tragen die Kennzeichnung „extern“ mit Quelle.
  it('kennzeichnet externe Einträge der Zeitleiste mit „extern“ und Quelle, eigene nicht', () => {
    i18n.global.locale.value = 'de'
    const ext = { ID: 'ext-1', Title: 'Fremd', Message: 'm', Status: 'investigating', StartedAt: today, external: { source_name: 'Example Cloud' } }
    const w = mountList({ timelineIncidents: [ext, incidents[0]] })
    const chips = w.findAll('.pl-ext-chip')
    expect(chips).toHaveLength(1)
    expect(chips[0].text()).toContain('extern')
    expect(chips[0].text()).toContain('Quelle: Example Cloud')
  })

  // Nachrichten eigener Vorfälle sind formatiert, Texte externer
  // Quellen bleiben Klartext (kein Markdown aus Fremdtext).
  it('zeigt externe Meldungen als Klartext und eigene Vorfälle formatiert', () => {
    const items = [
      { ID: 20, Title: 'Eigen', Message: '**fett**', Status: 'investigating', Components: [], CreatedAt: today },
      { ID: 'ext-1', Title: 'Extern', Message: '**roh** <b>x</b>', Status: 'investigating', Components: [], CreatedAt: today, external: { source_name: 'Quelle' } },
    ]
    const w = mountList({ timelineIncidents: items })
    const msgs = w.findAll('.pl-incident-msg')
    expect(msgs[0].find('strong').exists()).toBe(true)
    expect(msgs[1].find('strong').exists()).toBe(false)
    expect(msgs[1].text()).toBe('**roh** <b>x</b>')
  })
})
