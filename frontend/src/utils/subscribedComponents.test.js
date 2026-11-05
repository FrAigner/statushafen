import { describe, it, expect } from 'vitest'
import { idsFromCsv, csvFromIds, componentLabelsFromCsv } from './subscribedComponents'

describe('idsFromCsv', () => {
  it('parst eine kommaseparierte ID-Liste', () => {
    expect(idsFromCsv('1,3,5')).toEqual(['1', '3', '5'])
  })

  it('liefert ein leeres Array für leer/undefined (= alle Komponenten)', () => {
    expect(idsFromCsv('')).toEqual([])
    expect(idsFromCsv(undefined)).toEqual([])
  })
})

describe('csvFromIds', () => {
  it('baut den kommaseparierten String für die API', () => {
    expect(csvFromIds(['1', '3', '5'])).toBe('1,3,5')
  })

  it('liefert einen leeren String für ein leeres/undefined Array', () => {
    expect(csvFromIds([])).toBe('')
    expect(csvFromIds(undefined)).toBe('')
  })
})

describe('componentLabelsFromCsv', () => {
  const components = [
    { ID: 1, Name: 'API' },
    { ID: 3, Name: 'Datenbank' },
  ]

  it('mappt IDs auf Komponentennamen', () => {
    expect(componentLabelsFromCsv('1,3', components)).toBe('API, Datenbank')
  })

  it('zeigt "alle" für leer/undefined', () => {
    expect(componentLabelsFromCsv('', components)).toBe('alle')
    expect(componentLabelsFromCsv(undefined, components)).toBe('alle')
  })

  it('fällt für unbekannte IDs auf die rohe ID zurück statt sie zu verschlucken', () => {
    expect(componentLabelsFromCsv('1,999', components)).toBe('API, 999')
  })
})
