import { describe, it, expect, vi } from 'vitest'
import { availabilityDayMeta, durationParts, formatAvailability, formatDay, parseAvailability } from './availability'

// 4a: Zuordnung der Tagesstatus des 90-Tage-Endpunkts.
describe('availabilityDayMeta', () => {
  it.each([
    ['operational', 'ok', 'status.operational'],
    ['degraded_performance', 'deg', 'status.degradedPerformance'],
    ['partial_outage', 'part', 'status.partialOutage'],
    ['major_outage', 'major', 'status.majorOutage'],
    ['under_maintenance', 'maint', 'status.maintenance'],
    ['no_data', 'none', 'availability.noData'],
  ])('%s -> %s', (status, level, labelKey) => {
    expect(availabilityDayMeta(status)).toEqual({ level, labelKey })
  })

  it('behandelt unbekannte und fehlende Werte neutral wie no_data', () => {
    expect(availabilityDayMeta('weird').level).toBe('none')
    expect(availabilityDayMeta(undefined).level).toBe('none')
    // Prototyp-Schlüssel dürfen keine Statusfarbe liefern
    expect(availabilityDayMeta('constructor').level).toBe('none')
  })
})

describe('formatAvailability', () => {
  it('formatiert mit zwei Nachkommastellen nach Sprache', () => {
    expect(formatAvailability(99.84, 'de-DE')).toMatch(/^99,84\s%$/)
    expect(formatAvailability(99.84, 'en-US')).toMatch(/^99\.84\s%$/)
    expect(formatAvailability(100, 'de-DE')).toMatch(/^100,00\s%$/)
    expect(formatAvailability(0, 'de-DE')).toMatch(/^0,00\s%$/)
  })

  it('schneidet ab statt zu runden, damit Ausfallzeit nie als 100 % erscheint', () => {
    expect(formatAvailability(99.996, 'de-DE')).toMatch(/^99,99\s%$/)
    expect(formatAvailability(99.999, 'en-US')).toMatch(/^99\.99\s%$/)
  })

  it('liefert null ohne Daten, nie 0 % oder 100 %', () => {
    expect(formatAvailability(null, 'de-DE')).toBeNull()
    expect(formatAvailability(undefined, 'de-DE')).toBeNull()
    expect(formatAvailability(Number.NaN, 'de-DE')).toBeNull()
    expect(formatAvailability('99', 'de-DE')).toBeNull()
  })
})

describe('formatDay', () => {
  it('formatiert den UTC-Tag lokalisiert, unabhängig von der Zeitzone', () => {
    expect(formatDay('2026-10-29', 'de-DE')).toBe('29.10.2026')
    expect(formatDay('2026-10-29', 'en-US')).toBe('Oct 29, 2026')
  })
})

// Review-Befund 4 (PageLayoutList): 90 Tage × Komponenten dürfen nicht je Tag
// einen neuen Intl-Formatter bauen.
describe('Formatter-Cache', () => {
  it('legt je Sprache nur einen Formatter an', () => {
    const dtf = vi.spyOn(Intl, 'DateTimeFormat')
    const nf = vi.spyOn(Intl, 'NumberFormat')
    for (let i = 0; i < 50; i++) {
      formatDay('2026-10-28', 'fr-FR')
      formatAvailability(99.5, 'fr-FR')
    }
    expect(dtf.mock.calls.filter(c => c[0] === 'fr-FR')).toHaveLength(1)
    expect(nf.mock.calls.filter(c => c[0] === 'fr-FR')).toHaveLength(1)
    dtf.mockRestore()
    nf.mockRestore()
  })
})

describe('parseAvailability', () => {
  it('akzeptiert eine Antwort mit Tagen', () => {
    const res = { days: [{ date: '2026-10-29', status: 'operational', availability: 100 }] }
    expect(parseAvailability(res)).toBe(res)
  })

  it.each([null, undefined, {}, { days: null }, { days: [] }, { days: 'x' }])('verwirft %j', (res) => {
    expect(parseAvailability(res)).toBeNull()
  })
})

// Dauer je Schweregrad im Tooltip.
describe('durationParts', () => {
  it.each([
    [0, 'availability.durationLessThanMinute', {}],
    [59, 'availability.durationLessThanMinute', {}],
    [60, 'availability.durationMinutes', { minutes: 1 }],
    [3599, 'availability.durationMinutes', { minutes: 59 }],
    [3600, 'availability.durationHoursMinutes', { hours: 1, minutes: 0 }],
    [9000, 'availability.durationHoursMinutes', { hours: 2, minutes: 30 }],
    [86400, 'availability.durationHoursMinutes', { hours: 24, minutes: 0 }],
  ])('%s s -> %s', (seconds, key, params) => {
    expect(durationParts(seconds)).toEqual({ key, params })
  })

  it('behandelt ungültige Werte wie 0', () => {
    for (const bad of [-5, null, undefined, 'x', Number.NaN]) {
      expect(durationParts(bad).key).toBe('availability.durationLessThanMinute')
    }
  })
})
