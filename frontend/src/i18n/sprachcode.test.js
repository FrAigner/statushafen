import { describe, it, expect } from 'vitest'
import de from './locales/de.json'
import en from './locales/en.json'

// The field said "ISO 639-1", but the backend allows
// ^[a-z]{2,3}(-[a-z0-9]{2,8})*$ (2-3 letters, optional subtag like en-us).
describe('Beschriftung des Sprachcodes', () => {
  it.each([
    ['de', de, 'Sprachcode (z.B. de, en-us)'],
    ['en', en, 'Language code (e.g. de, en-us)'],
  ])('%s nennt das erlaubte Format statt ISO 639-1', (_l, loc, expected) => {
    expect(loc.languages.fieldCode).not.toMatch(/ISO[\s-]?639/i)
    expect(loc.languages.fieldCode).toBe(expected)
  })
})
