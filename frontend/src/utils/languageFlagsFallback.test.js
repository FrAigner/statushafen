import { describe, it, expect, vi, afterEach } from 'vitest'
import { languageDisplayName } from './languageFlags'

// Liefert Intl.DisplayNames keinen Namen (undefined), zeigt die Tabelle den Code.
describe('languageDisplayName: Rückfall ohne Namen', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('gibt den Code zurück, wenn Intl.DisplayNames nichts liefert', () => {
    vi.stubGlobal('Intl', { ...Intl, DisplayNames: class { of() { return undefined } } })
    expect(languageDisplayName('x1', 'de')).toBe('x1')
  })
})
