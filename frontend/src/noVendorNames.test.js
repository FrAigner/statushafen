import { describe, it, expect } from 'vitest'
import de from './i18n/locales/de.json'
import en from './i18n/locales/en.json'

// In Hilfe und Oberflächentexten stehen keine Anbieternamen als Beispiel. Die Schnittstelle
// heißt "Summary-API v2". Eine Seite der Hilfe (externe Quellen) darf erklären, dass sie mit
// Statusseiten auf Basis von Atlassian Statuspage kompatibel ist, und stellt klar, dass das
// Projekt nicht mit Atlassian verbunden ist. Tests dürfen Anbieternamen nicht als Testdaten nutzen.
const docs = import.meta.glob('./docs/**/*.md', { query: '?raw', import: 'default', eager: true })
const VENDORS = /stackit|instatus/i
const ATLASSIAN_PAGES = ['./docs/de/externe-quellen.md', './docs/en/externe-quellen.md']

describe('Keine Anbieternamen in Hilfe und Texten', () => {
  it('Hilfeseiten (de/en) nennen STACKIT und Instatus nicht', () => {
    const hits = Object.entries(docs).filter(([, text]) => VENDORS.test(text)).map(([file]) => file)
    expect(hits).toEqual([])
  })

  it.each([['de', de], ['en', en]])('Oberflächentexte (%s) nennen keinen Anbieter', (_, messages) => {
    const json = JSON.stringify(messages)
    expect(json).not.toMatch(VENDORS)
    expect(json).not.toMatch(/atlassian/i)
  })

  it('Atlassian steht nur in der Hilfe zu externen Quellen', () => {
    const hits = Object.entries(docs).filter(([, text]) => /atlassian/i.test(text)).map(([file]) => file).sort()
    expect(hits).toEqual(ATLASSIAN_PAGES)
  })

  it.each(ATLASSIAN_PAGES)('%s nennt die Summary-API v2 und stellt klar, dass keine Verbindung zu Atlassian besteht', (page) => {
    const text = docs[page]
    expect(text).toMatch(/Summary[- ]API v2/)
    expect(text).toMatch(/not affiliated with|keine Verbindung zu Atlassian/i)
  })
})
