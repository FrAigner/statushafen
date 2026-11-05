import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import AdminMonitoring from './AdminMonitoring.vue'
import { ERROR_RATE_DEGRADED, ERROR_RATE_MAJOR, errorLevel } from './monitoringThresholds.js'
import { axe } from '../../test-support/axe.js'

function monitoringFixture() {
  return {
    routes: [
      { method: 'GET', route: '/api/pages', request_count: 120, requests_per_minute: 4.2, error_rate: 0.15, avg_latency_ms: 45, p50_latency_ms: 30, p95_latency_ms: 1200, max_latency_ms: 2000 },
    ],
    totals: {
      request_count: 500, error_count: 10, error_rate: 0.02, requests_per_minute: 12.4,
      avg_latency_ms: 55, p50_latency_ms: 40, p95_latency_ms: 900, max_latency_ms: 1800,
    },
    uptime_seconds: 7500,
    saturation: {
      goroutine_count: 24, heap_alloc_bytes: 52428800, heap_sys_bytes: 0, sys_bytes: 0, num_gc: 3,
      db_stats_available: true, db_max_open_connections: 10, db_open_connections: 4, db_in_use: 9, db_idle: 1,
    },
  }
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

let activeWrapper = null

async function mountMonitoring(fetchImpl) {
  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse(monitoringFixture()))
  const wrapper = mount(AdminMonitoring, { global: { plugins: [i18n] } })
  await flushPromises()
  activeWrapper = wrapper
  return wrapper
}

describe('AdminMonitoring', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('rendert die Request-KPIs formatiert (Prozent, ms, Uptime)', async () => {
    const wrapper = await mountMonitoring()

    const values = wrapper.findAll('.kpi__value').map(el => el.text())
    expect(values).toContain('500')
    expect(values).toContain('2.0%')
    expect(values).toContain('55ms')
    expect(values).toContain('2h 5min')
  })

  it('formatiert Latenzen >= 1s in Sekunden', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(),
      totals: { ...monitoringFixture().totals, avg_latency_ms: 1500 },
    })))

    expect(wrapper.findAll('.kpi__value').map(el => el.text())).toContain('1.50s')
  })

  // Link kam fest verdrahtet auf „statushafen-staging“, auch in Prod.
  it('zeigt den Grafana-Link mit der vom Backend gelieferten URL', async () => {
    const url = 'https://g.example/d/abc?var-env=statushafen-prod'
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(), grafana_dashboard_url: url,
    })))

    const link = wrapper.find('a.grafana-link')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toBe(url)
    expect(wrapper.html()).not.toContain('statushafen-staging')
  })

  it('blendet den Grafana-Link aus, wenn keine URL konfiguriert ist', async () => {
    const withoutField = await mountMonitoring()
    expect(withoutField.find('a.grafana-link').exists()).toBe(false)
    withoutField.unmount()

    const emptyField = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(), grafana_dashboard_url: '',
    })))
    expect(emptyField.find('a.grafana-link').exists()).toBe(false)
  })

  it('zeigt die DB-Pool-Auslastung, wenn db_stats_available true ist', async () => {
    const wrapper = await mountMonitoring()

    expect(wrapper.text()).toContain('9 / 10')
  })

  it('zeigt "n/a" für den DB-Pool, wenn keine DB-Stats verfügbar sind', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(),
      saturation: { ...monitoringFixture().saturation, db_stats_available: false },
    })))

    expect(wrapper.text()).toContain('n/a')
  })

  it('rendert eine Tabellenzeile je Route mit Methode und formatierten Werten', async () => {
    const wrapper = await mountMonitoring()

    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('GET')
    expect(row.text()).toContain('/api/pages')
    expect(row.text()).toContain('120')
    expect(row.text()).toContain('15.0%')
  })

  it('zeigt den Empty-State ohne Routen', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(), routes: [],
    })))

    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.find('tbody tr').exists()).toBe(false)
  })

  // Stufe 4d: h2 statt h1 (die Topbar trägt die h1); Kennzahlen als
  // KpiCard ohne Inline-Farben, Alarm nur bei Handlungsbedarf (Text für Screenreader).
  it('rendert den Titel als h2 ohne h1 und ohne Inline-Styles', async () => {
    const wrapper = await mountMonitoring()

    expect(wrapper.find('h2.page-head__title').exists()).toBe(true)
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('style=')
  })

  it('markiert nur die DB-Kachel als Alarm, wenn der Pool zu 90 % belegt ist', async () => {
    const wrapper = await mountMonitoring()

    const alarme = wrapper.findAll('.kpi--alert')
    expect(alarme).toHaveLength(1)
    expect(alarme[0].text()).toContain('9 / 10')
    expect(alarme[0].text()).toContain('Handlungsbedarf')
  })

  it('markiert keine Kachel, wenn Fehlerquote und DB-Pool unauffällig sind', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(),
      saturation: { ...monitoringFixture().saturation, db_in_use: 2 },
    })))

    expect(wrapper.find('.kpi--alert').exists()).toBe(false)
  })

  it('markiert eine Gesamt-Fehlerquote ab 10 % als Alarm', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(),
      totals: { ...monitoringFixture().totals, error_rate: 0.1 },
      saturation: { ...monitoringFixture().saturation, db_stats_available: false },
    })))

    const alarme = wrapper.findAll('.kpi--alert')
    expect(alarme).toHaveLength(1)
    expect(alarme[0].text()).toContain('10.0%')
  })

  it('zeigt die Fehlerquote je Route als StatusBadge: ab 10 % major, darunter degraded, bei 0 als Text', async () => {
    const route = (path, error_rate) => ({ ...monitoringFixture().routes[0], route: path, error_rate })
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(),
      routes: [route('/a', 0.15), route('/b', 0.06), route('/c', 0)],
    })))

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('.st-badge--major').text()).toContain('15.0%')
    expect(rows[1].find('.st-badge--degraded').text()).toContain('6.0%')
    expect(rows[2].find('.st-badge').exists()).toBe(false)
    expect(rows[2].text()).toContain('0.0%')
  })

  // Regression: schon jede Quote > 0 zeigte gelb. Schwellen: gelb ab 5 %, rot ab 10 %.
  it.each([
    [0, null],
    [0.001, null],
    [0.049, null],
    [0.05, 'degraded'],
    [0.099, 'degraded'],
    [0.1, 'major'],
    [0.5, 'major'],
  ])('Tabelle: Fehlerquote %s -> Marke %s', async (rate, level) => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(),
      routes: [{ ...monitoringFixture().routes[0], error_rate: rate }],
    })))
    const row = wrapper.find('tbody tr')
    expect(row.find('.st-badge').exists()).toBe(level !== null)
    if (level) expect(row.find(`.st-badge--${level}`).exists()).toBe(true)
  })

  it('KPI-Kachel: Alarm erst ab 10 %, nicht bei 9,9 %', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      ...monitoringFixture(),
      totals: { ...monitoringFixture().totals, error_rate: 0.099 },
      saturation: { ...monitoringFixture().saturation, db_stats_available: false },
    })))
    expect(wrapper.find('.kpi--alert').exists()).toBe(false)
  })

  it('Tabelle nennt in Kopf-Tooltip, dass nur 5xx als Fehler zaehlen', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse(monitoringFixture())))
    expect(wrapper.html()).toContain(i18n.global.t('monitoring.errorRateHint'))
    expect(i18n.global.t('monitoring.errorRateHint')).toContain('5xx')
  })

  it('exportiert die Schwellen als benannte Konstanten', () => {
    expect(ERROR_RATE_DEGRADED).toBe(0.05)
    expect(ERROR_RATE_MAJOR).toBe(0.1)
    expect(errorLevel(0.0499)).toBeNull()
    expect(errorLevel(0.05)).toBe('degraded')
    expect(errorLevel(0.1)).toBe('major')
  })

  it('zeigt bei fehlenden Kennzahlen Nullwerte statt "undefined"', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({
      routes: [],
      totals: { request_count: undefined, error_rate: undefined, requests_per_minute: 0 },
      saturation: { db_stats_available: false },
    })))

    const werte = wrapper.findAll('.kpi__value').map(el => el.text())
    expect(werte).toContain('0')
    expect(werte).toContain('–')
    expect(werte.join(' ')).not.toContain('undefined')
    expect(wrapper.find('.kpi--alert').exists()).toBe(false)
  })

  it('zeigt den Ladezustand, solange die Daten fehlen', async () => {
    const wrapper = await mountMonitoring(vi.fn(() => new Promise(() => {})))

    expect(wrapper.text()).toContain('Lädt')
    expect(wrapper.find('.empty-state').exists()).toBe(false)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({ error: 'kaputt' }, 500)))

    expect(wrapper.find('.alert-error').text()).toContain('kaputt')
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = await mountMonitoring()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Richtung C: Brotkrumen und die Endpunkt-Tabelle in einer Listenkarte.
  it('zeigt Brotkrumen und die Endpunkt-Tabelle in einer Karte mit Titel', async () => {
    const wrapper = await mountMonitoring()

    expect(wrapper.find('.page-head__crumbs').text()).toContain('Administration')
    expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe('Monitoring')
    expect(wrapper.find('section.comp-card h3').text()).toBe('Endpunkte')
    expect(wrapper.find('section.comp-card table').exists()).toBe(true)
  })

  it('zeigt den Leerzustand in der Karte, wenn keine Endpunkte aufgezeichnet sind', async () => {
    const wrapper = await mountMonitoring(vi.fn().mockResolvedValue(jsonResponse({ ...monitoringFixture(), routes: [] })))

    expect(wrapper.find('section.comp-card .empty-state').text()).toContain('Noch keine Requests aufgezeichnet.')
    expect(wrapper.find('table').exists()).toBe(false)
  })

  // ungenutzte Routen (0 Requests) werden gezeigt, mit "–" statt Nullwerten, und lassen sich ausblenden.
  describe('ungenutzte Endpunkte', () => {
    const withUnused = () => ({
      ...monitoringFixture(),
      routes: [
        ...monitoringFixture().routes,
        { method: 'DELETE', route: '/api/tags/:id', request_count: 0, error_count: 0, error_rate: 0, requests_per_minute: 0, avg_latency_ms: 0, p50_latency_ms: 0, p95_latency_ms: 0, max_latency_ms: 0 },
        { method: 'PUT', route: '/api/tags/:id', request_count: 0, error_rate: 0, requests_per_minute: 0, avg_latency_ms: 0, p50_latency_ms: 0, p95_latency_ms: 0, max_latency_ms: 0 },
      ],
    })
    const mountUnused = (body = withUnused()) => mountMonitoring(vi.fn().mockResolvedValue(jsonResponse(body)))
    const bodyRows = (w) => w.findAll('tbody tr')

    it('zeigt ungenutzte Routen standardmäßig mit "–" statt 0,0 % und 0ms', async () => {
      const wrapper = await mountUnused()

      expect(bodyRows(wrapper)).toHaveLength(3)
      const unused = bodyRows(wrapper).find(r => r.text().includes('DELETE'))
      expect(unused.text()).toContain('/api/tags/:id')
      expect(unused.text()).not.toContain('0.0%')
      expect(unused.text()).not.toContain('0ms')
      expect(unused.text()).toContain('–')
      expect(unused.find('.status-badge').exists()).toBe(false)
      // genutzte Zeile bleibt unverändert
      const used = bodyRows(wrapper).find(r => r.text().includes('/api/pages'))
      expect(used.text()).toContain('15.0%')
    })

    it('weist auf die Anzahl ungenutzter Endpunkte hin', async () => {
      const wrapper = await mountUnused()
      expect(wrapper.find('.unused-hint').text()).toContain('2 Endpunkte ohne Request')
    })

    it('blendet ungenutzte Routen mit "Nur genutzte" aus und mit "Alle" wieder ein', async () => {
      const wrapper = await mountUnused()
      const buttons = wrapper.findAll('.segment__item')
      expect(buttons.map(b => b.text())).toEqual(['Alle', 'Nur genutzte'])

      await buttons[1].trigger('click')
      expect(bodyRows(wrapper)).toHaveLength(1)
      expect(wrapper.find('tbody').text()).not.toContain('DELETE')

      await buttons[0].trigger('click')
      expect(bodyRows(wrapper)).toHaveLength(3)
    })

    it('zeigt einen Hinweis statt leerer Tabelle, wenn nichts genutzt wurde', async () => {
      const body = { ...withUnused(), routes: withUnused().routes.slice(1) }
      const wrapper = await mountUnused(body)
      await wrapper.findAll('.segment__item')[1].trigger('click')

      expect(wrapper.find('table').exists()).toBe(false)
      expect(wrapper.text()).toContain('Keine Endpunkte mit Requests seit dem Start.')
    })

    it('zeigt keinen Hinweis, wenn alle Routen genutzt sind', async () => {
      const wrapper = await mountMonitoring()
      expect(wrapper.find('.unused-hint').exists()).toBe(false)
    })
  })
})
