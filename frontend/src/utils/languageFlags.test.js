import { describe, it, expect } from 'vitest'
import { languageFlag, languageDisplayName } from './languageFlags'

const BADGE = (country) => [...country].map(c => String.fromCodePoint(0x1f1e6 + c.charCodeAt(0) - 65)).join('')

describe('languageFlag', () => {
  it('liefert die Flagge als Regionalzeichen-Paar', () => {
    expect(languageFlag('de')).toBe(BADGE('DE'))
    expect(languageFlag('bg')).toBe(BADGE('BG'))
  })
  it('bildet Sprache auf das typische Land ab, nicht auf den Sprachcode', () => {
    expect(languageFlag('en')).toBe(BADGE('GB'))
    expect(languageFlag('ga')).toBe(BADGE('IE'))
    expect(languageFlag('lb')).toBe(BADGE('LU'))
    expect(languageFlag('rm')).toBe(BADGE('CH'))
    expect(languageFlag('sl')).toBe(BADGE('SI'))
    expect(languageFlag('el')).toBe(BADGE('GR'))
  })
  it('kennt alle 30 Standardsprachen des Katalogs', () => {
    const codes = 'bs bg da de en et fi fr el ga it hr lv lt lb mt mk nl pl pt rm ro sv sr sk sl es cs tr hu'.split(' ')
    expect(codes).toHaveLength(30)
    for (const c of codes) expect(languageFlag(c), c).not.toBe('')
  })
  it('nutzt bei Untertags die Basissprache, ignoriert Groß-/Kleinschreibung', () => {
    expect(languageFlag('EN-us')).toBe(BADGE('GB'))
    expect(languageFlag('pt-BR')).toBe(BADGE('PT'))
  })
  it('liefert für unbekannte oder leere Codes keine Flagge', () => {
    expect(languageFlag('x1')).toBe('')
    expect(languageFlag('')).toBe('')
    expect(languageFlag(undefined)).toBe('')
  })
})

describe('languageDisplayName', () => {
  it('nennt die Sprache auf Deutsch und Englisch', () => {
    expect(languageDisplayName('bg', 'de')).toBe('Bulgarisch')
    expect(languageDisplayName('bg', 'en')).toBe('Bulgarian')
    expect(languageDisplayName('de', 'en')).toBe('German')
  })
  it('fällt bei ungültigem Code oder Ort auf den Code zurück', () => {
    expect(languageDisplayName('###', 'de')).toBe('###')
    expect(languageDisplayName('de', '###')).toBe('de')
  })
})
