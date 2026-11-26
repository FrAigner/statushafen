import { describe, it, expect, vi, afterEach } from 'vitest'
import { languageDisplayName } from './languageFlags'

// If Intl.DisplayNames returns no name (undefined), the table shows the code.
describe('languageDisplayName: Rückfall ohne Namen', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('gibt den Code zurück, wenn Intl.DisplayNames nichts liefert', () => {
    vi.stubGlobal('Intl', { ...Intl, DisplayNames: class { of() { return undefined } } })
    expect(languageDisplayName('x1', 'de')).toBe('x1')
  })
})
