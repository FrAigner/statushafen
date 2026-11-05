import { describe, it, expect } from 'vitest'
import { STATUS_LEVELS, STATUS_MAINTENANCE, normalizeStatus, toRawStatus, statusMeta } from './status'

describe('normalizeStatus', () => {
  it.each([
    ['operational', 'operational'],
    ['degraded_performance', 'degraded'],
    ['degraded', 'degraded'],
    ['partial_outage', 'partial'],
    ['partial', 'partial'],
    ['major_outage', 'major'],
    ['major', 'major'],
    ['maintenance', 'maintenance'],
    // Rohwert des Backends (vorher neutral mit Rohtext).
    ['under_maintenance', 'maintenance'],
  ])('%s -> %s', (value, level) => {
    expect(normalizeStatus(value)).toBe(level)
  })

  it('liefert null für Unbekanntes, leere Werte und Prototyp-Namen', () => {
    for (const value of ['kaputt', '', undefined, null, 'constructor', 'toString']) {
      expect(normalizeStatus(value)).toBeNull()
    }
  })
})

describe('toRawStatus', () => {
  it('bildet jede Stufe auf den Backend-Rohwert ab, auch aus Kurzformen', () => {
    expect(STATUS_LEVELS.map(toRawStatus)).toEqual(['operational', 'degraded_performance', 'partial_outage', 'major_outage'])
    expect(toRawStatus('major_outage')).toBe('major_outage')
  })

  it('liefert null für Wartung und Unbekanntes (kein Komponentenstatus)', () => {
    expect(toRawStatus(STATUS_MAINTENANCE)).toBeNull()
    expect(toRawStatus('kaputt')).toBeNull()
  })
})

describe('statusMeta', () => {
  it('liefert Stufe, Form und Wortlaut-Schlüssel, vier Stufen plus Wartung', () => {
    expect(statusMeta('operational')).toEqual({ level: 'operational', shape: 'circle-check', labelKey: 'status.operational' })
    expect(statusMeta('degraded_performance')).toEqual({ level: 'degraded', shape: 'triangle-exclamation', labelKey: 'status.degradedPerformance' })
    expect(statusMeta('partial_outage')).toEqual({ level: 'partial', shape: 'diamond-minus', labelKey: 'status.partialOutage' })
    expect(statusMeta('major_outage')).toEqual({ level: 'major', shape: 'octagon-cross', labelKey: 'status.majorOutage' })
    expect(statusMeta('maintenance')).toEqual({ level: 'maintenance', shape: 'square-clock', labelKey: 'status.maintenance' })
  })

  it('jede Stufe hat eine eigene Form (Status nie nur über Farbe)', () => {
    const shapes = [...STATUS_LEVELS, STATUS_MAINTENANCE].map(s => statusMeta(s).shape)
    expect(new Set(shapes).size).toBe(5)
  })

  it('unbekannte Werte bekommen eine neutrale Darstellung ohne Statusfarbe', () => {
    expect(statusMeta('kaputt')).toEqual({ level: null, shape: 'circle', labelKey: null })
  })
})
