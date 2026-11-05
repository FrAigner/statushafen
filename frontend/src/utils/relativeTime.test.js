import { describe, it, expect } from 'vitest'
import { formatRelative } from './relativeTime'

const NOW = Date.parse('2026-10-30T12:00:00Z')
const ago = seconds => new Date(NOW - seconds * 1000).toISOString()

describe('formatRelative', () => {
  it('wählt die Einheit nach Größenordnung (de)', () => {
    expect(formatRelative(ago(30), 'de', NOW)).toBe('vor 30 Sekunden')
    expect(formatRelative(ago(5 * 60), 'de', NOW)).toBe('vor 5 Minuten')
    expect(formatRelative(ago(3 * 3600), 'de', NOW)).toBe('vor 3 Stunden')
    expect(formatRelative(ago(3 * 86400), 'de', NOW)).toBe('vor 3 Tagen')
    expect(formatRelative(ago(65 * 86400), 'de', NOW)).toBe('vor 2 Monaten')
    expect(formatRelative(ago(800 * 86400), 'de', NOW)).toBe('vor 2 Jahren')
  })

  it('trifft die Grenzen genau (59 s, 60 s, 23 h, 24 h, 29 d, 30 d, 364 d, 365 d)', () => {
    expect(formatRelative(ago(59), 'de', NOW)).toBe('vor 59 Sekunden')
    expect(formatRelative(ago(60), 'de', NOW)).toBe('vor 1 Minute')
    expect(formatRelative(ago(23 * 3600 + 3599), 'de', NOW)).toBe('vor 23 Stunden')
    expect(formatRelative(ago(24 * 3600), 'de', NOW)).toBe('gestern')
    expect(formatRelative(ago(29 * 86400 + 86399), 'de', NOW)).toBe('vor 29 Tagen')
    expect(formatRelative(ago(30 * 86400), 'de', NOW)).toBe('letzten Monat')
    expect(formatRelative(ago(364 * 86400), 'de', NOW)).toBe('vor 12 Monaten')
    expect(formatRelative(ago(365 * 86400), 'de', NOW)).toBe('letztes Jahr')
  })

  it('folgt der Sprache (en)', () => {
    expect(formatRelative(ago(3 * 86400), 'en', NOW)).toBe('3 days ago')
  })

  it('behandelt Zukunft wie „gerade eben“', () => {
    expect(formatRelative(ago(-120), 'de', NOW)).toBe('jetzt')
  })

  it('liefert für fehlende oder ungültige Werte einen leeren Text', () => {
    expect(formatRelative(undefined, 'de', NOW)).toBe('')
    expect(formatRelative(null, 'de', NOW)).toBe('')
    expect(formatRelative('kein Datum', 'de', NOW)).toBe('')
  })

  it('nutzt ohne Angabe die aktuelle Zeit', () => {
    expect(formatRelative(new Date().toISOString(), 'de')).toBe('jetzt')
  })
})
