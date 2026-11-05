import { describe, it, expect } from 'vitest'
import {
  normalizePageLayout,
  overallStatus,
  pageOverallStatus,
  countLeaves,
  OVERALL_TEXT_KEYS,
  groupIncidentsByDay,
  eventTime,
} from './pageLayout'

describe('normalizePageLayout', () => {
  it('lässt liste und standard unverändert', () => {
    expect(normalizePageLayout('liste')).toBe('liste')
    expect(normalizePageLayout('standard')).toBe('standard')
  })
  it('fällt bei fehlendem oder unbekanntem Wert auf standard zurück', () => {
    expect(normalizePageLayout(undefined)).toBe('standard')
    expect(normalizePageLayout(null)).toBe('standard')
    expect(normalizePageLayout('')).toBe('standard')
    expect(normalizePageLayout('Liste')).toBe('standard')
    expect(normalizePageLayout('x')).toBe('standard')
  })
})

describe('overallStatus', () => {
  it('ist operational ohne Komponenten', () => {
    expect(overallStatus([])).toBe('operational')
    expect(overallStatus(undefined)).toBe('operational')
  })
  it('nimmt den schlechtesten Status, auch aus Kindern', () => {
    const tree = [
      { Status: 'operational', Children: [{ Status: 'degraded_performance' }] },
      { Status: 'partial_outage' },
    ]
    expect(overallStatus(tree)).toBe('partial_outage')
    expect(overallStatus([{ Status: 'operational', Children: [{ Status: 'major_outage', Children: [] }] }])).toBe('major_outage')
    expect(overallStatus([{ Status: 'degraded_performance' }, { Status: 'operational' }])).toBe('degraded_performance')
  })
  it('ignoriert unbekannte Status', () => {
    expect(overallStatus([{ Status: 'weird' }])).toBe('operational')
  })
})

// 4a: Gesamtstatus oben mit Wartung als eigenem Zustand.
describe('pageOverallStatus', () => {
  const ok = [{ Status: 'operational' }]
  const running = { IsMaintenance: true, Status: 'in_progress' }

  it('entspricht overallStatus ohne laufende Wartung', () => {
    expect(pageOverallStatus(ok, [])).toBe('operational')
    expect(pageOverallStatus(ok, undefined)).toBe('operational')
    expect(pageOverallStatus([{ Status: 'partial_outage' }], [])).toBe('partial_outage')
  })
  it('meldet maintenance, wenn alles läuft und eine Wartung läuft', () => {
    expect(pageOverallStatus(ok, [running])).toBe('maintenance')
  })
  it('meldet maintenance auch für eine Komponente in Wartung ohne eigenen Vorfall (extern gekoppelt)', () => {
    expect(pageOverallStatus([{ Status: 'operational' }, { Status: 'under_maintenance' }], [])).toBe('maintenance')
    expect(pageOverallStatus([{ Status: 'under_maintenance' }, { Status: 'partial_outage' }], [])).toBe('partial_outage')
  })
  it('lässt eine Störung vor der Wartung gewinnen', () => {
    expect(pageOverallStatus([{ Status: 'degraded_performance' }], [running])).toBe('degraded_performance')
  })
  it('zählt geplante, behobene und Nicht-Wartungs-Vorfälle nicht als laufende Wartung', () => {
    const others = [
      { IsMaintenance: true, Status: 'scheduled' },
      { IsMaintenance: true, Status: 'resolved' },
      { IsMaintenance: false, Status: 'in_progress' },
    ]
    expect(pageOverallStatus(ok, others)).toBe('operational')
  })
})

describe('OVERALL_TEXT_KEYS', () => {
  it('hat zu jedem möglichen Gesamtstatus einen Text', () => {
    for (const status of ['operational', 'degraded_performance', 'partial_outage', 'major_outage', 'maintenance']) {
      expect(OVERALL_TEXT_KEYS[status]).toMatch(/^pageLayout\.overall/)
    }
    expect(pageOverallStatus([{ Status: 'weird' }], [])).toBe('operational')
    expect(OVERALL_TEXT_KEYS[pageOverallStatus([{ Status: 'weird' }], [])]).toBeDefined()
  })
})

describe('countLeaves', () => {
  it('zählt nur Blätter, Gruppen nicht', () => {
    const tree = [
      { Status: 'operational', Children: [{ Status: 'operational' }, { Status: 'major_outage', Children: [] }] },
      { Status: 'degraded_performance' },
    ]
    expect(countLeaves(tree)).toEqual({ total: 3, operational: 1 })
  })
  it('liefert 0 von 0 ohne Komponenten', () => {
    expect(countLeaves([])).toEqual({ total: 0, operational: 0 })
    expect(countLeaves(undefined)).toEqual({ total: 0, operational: 0 })
  })
})

describe('groupIncidentsByDay', () => {
  it('gruppiert nach Tag, jüngster Tag und jüngster Vorfall zuerst', () => {
    const list = [
      { ID: 1, CreatedAt: '2026-10-08T08:00:00Z' },
      { ID: 2, CreatedAt: '2026-10-09T08:00:00Z' },
      { ID: 3, CreatedAt: '2026-10-08T18:00:00Z' },
    ]
    const groups = groupIncidentsByDay(list)
    expect(groups.map(g => g.date)).toEqual(['2026-10-09', '2026-10-08'])
    expect(groups[1].items.map(i => i.ID)).toEqual([3, 1])
  })
  it('liefert [] für leere Eingabe und ändert die Eingabe nicht', () => {
    expect(groupIncidentsByDay([])).toEqual([])
    const list = [{ ID: 1, CreatedAt: '2026-10-08T08:00:00Z' }, { ID: 2, CreatedAt: '2026-10-09T08:00:00Z' }]
    groupIncidentsByDay(list)
    expect(list.map(i => i.ID)).toEqual([1, 2])
  })
})

describe('Ereigniszeit statt Erstellzeit', () => {
  it('eventTime nimmt StartedAt, sonst CreatedAt', () => {
    expect(eventTime({ CreatedAt: '2026-10-22T01:00:00Z', StartedAt: '2026-10-02T08:00:00Z' })).toBe('2026-10-02T08:00:00Z')
    expect(eventTime({ CreatedAt: '2026-10-22T01:00:00Z', StartedAt: null })).toBe('2026-10-22T01:00:00Z')
  })
  it('eventTime nimmt bei Wartung ScheduledStartAt, sonst CreatedAt', () => {
    expect(eventTime({ IsMaintenance: true, CreatedAt: '2026-10-22T01:00:00Z', ScheduledStartAt: '2026-10-02T02:00:00Z' })).toBe('2026-10-02T02:00:00Z')
    expect(eventTime({ IsMaintenance: true, CreatedAt: '2026-10-22T01:00:00Z' })).toBe('2026-10-22T01:00:00Z')
  })
  it('groupIncidentsByDay gruppiert und sortiert nach Ereigniszeit', () => {
    const list = [
      { ID: 1, CreatedAt: '2026-10-22T08:00:00Z', StartedAt: '2026-10-02T08:00:00Z' },
      { ID: 2, CreatedAt: '2026-10-22T09:00:00Z' },
      { ID: 3, CreatedAt: '2026-10-22T10:00:00Z', IsMaintenance: true, ScheduledStartAt: '2026-10-05T10:00:00Z' },
    ]
    const groups = groupIncidentsByDay(list)
    expect(groups.map(g => g.date)).toEqual(['2026-10-22', '2026-10-05', '2026-10-02'])
    expect(groups.map(g => g.items[0].ID)).toEqual([2, 3, 1])
  })
})
