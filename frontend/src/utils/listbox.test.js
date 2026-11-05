import { describe, it, expect } from 'vitest'
import { isOpenKey, isSelectKey, nextActiveIndex } from './listbox'

describe('isOpenKey / isSelectKey', () => {
  it('öffnen: Pfeile, Enter, Leertaste', () => {
    for (const key of ['ArrowDown', 'ArrowUp', 'Enter', ' ']) expect(isOpenKey(key)).toBe(true)
    for (const key of ['Escape', 'Tab', 'a', 'Home']) expect(isOpenKey(key)).toBe(false)
  })

  it('übernehmen: nur Enter und Leertaste', () => {
    for (const key of ['Enter', ' ']) expect(isSelectKey(key)).toBe(true)
    for (const key of ['ArrowDown', 'Escape', 'Tab']) expect(isSelectKey(key)).toBe(false)
  })
})

describe('nextActiveIndex', () => {
  it('Pfeil runter/hoch bewegt um eins', () => {
    expect(nextActiveIndex(1, 'ArrowDown', 4)).toBe(2)
    expect(nextActiveIndex(2, 'ArrowUp', 4)).toBe(1)
  })

  it('bricht an den Enden nicht um', () => {
    expect(nextActiveIndex(3, 'ArrowDown', 4)).toBe(3)
    expect(nextActiveIndex(0, 'ArrowUp', 4)).toBe(0)
  })

  it('Home und End springen an Anfang und Ende', () => {
    expect(nextActiveIndex(2, 'Home', 4)).toBe(0)
    expect(nextActiveIndex(1, 'End', 4)).toBe(3)
  })

  it('andere Tasten navigieren nicht; leere Liste ebenfalls nicht', () => {
    expect(nextActiveIndex(1, 'a', 4)).toBeNull()
    expect(nextActiveIndex(0, 'ArrowDown', 0)).toBeNull()
  })

  it('eine einzelne Option bleibt aktiv', () => {
    expect(nextActiveIndex(0, 'ArrowDown', 1)).toBe(0)
    expect(nextActiveIndex(0, 'End', 1)).toBe(0)
  })
})
