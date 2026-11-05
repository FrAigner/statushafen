import { describe, it, expect } from 'vitest'
import { toLocalInputValue, formatDateTimeShort, toIntlLocale } from './datetime'

describe('toLocalInputValue', () => {
  it('formatiert einen ISO-Zeitstempel für <input type="datetime-local">', () => {
    const iso = '2026-03-05T08:07:00Z'
    const d = new Date(iso)
    const pad = (n) => String(n).padStart(2, '0')
    const expected = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`

    expect(toLocalInputValue(iso)).toBe(expected)
  })

  it('liefert einen leeren String für leer/undefined', () => {
    expect(toLocalInputValue('')).toBe('')
    expect(toLocalInputValue(undefined)).toBe('')
  })

  it('paddet einstellige Monats-/Tages-/Zeitanteile mit führender Null', () => {
    // Lokale Zeit statt UTC konstruieren, damit der Test zeitzonenunabhängig ist.
    const d = new Date(2026, 0, 5, 3, 4)
    expect(toLocalInputValue(d.toISOString())).toBe('2026-01-05T03:04')
  })
})

describe('toIntlLocale', () => {
  it('bildet die App-Locale "de"/"en" auf das passende BCP-47-Tag ab', () => {
    expect(toIntlLocale('de')).toBe('de-DE')
    expect(toIntlLocale('en')).toBe('en-US')
  })

  it('fällt bei fehlender/unbekannter Locale auf Deutsch zurück', () => {
    expect(toIntlLocale(undefined)).toBe('de-DE')
    expect(toIntlLocale('fr')).toBe('de-DE')
  })
})

describe('formatDateTimeShort', () => {
  it('formatiert einen ISO-Zeitstempel im deutschen Kurzformat, wenn keine Locale übergeben wird', () => {
    const iso = '2026-03-05T08:07:00Z'
    expect(formatDateTimeShort(iso)).toBe(new Date(iso).toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' }))
  })

  it('formatiert im englischen Kurzformat, wenn locale="en" übergeben wird (Datum blieb bei Sprachumschaltung deutsch)', () => {
    const iso = '2026-03-05T08:07:00Z'
    expect(formatDateTimeShort(iso, 'en')).toBe(new Date(iso).toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short' }))
  })

  it('liefert einen Gedankenstrich für leer/undefined', () => {
    expect(formatDateTimeShort('')).toBe('—')
    expect(formatDateTimeShort(undefined)).toBe('—')
  })
})
