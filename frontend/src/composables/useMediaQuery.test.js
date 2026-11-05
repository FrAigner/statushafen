import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useMediaQuery, useIsMobile } from './useMediaQuery'

function mockMatchMedia(matches) {
  const listeners = []
  const mql = {
    matches,
    media: '',
    addEventListener: vi.fn((event, cb) => listeners.push(cb)),
    removeEventListener: vi.fn((event, cb) => {
      const idx = listeners.indexOf(cb)
      if (idx !== -1) listeners.splice(idx, 1)
    }),
  }
  window.matchMedia = vi.fn(() => mql)
  return { mql, fireChange: (next) => { mql.matches = next; listeners.forEach(cb => cb({ matches: next })) } }
}

describe('useMediaQuery', () => {
  it('gibt den initialen matches-Zustand der Media Query zurück', () => {
    mockMatchMedia(true)
    const matches = useMediaQuery('(max-width: 767px)')
    expect(matches.value).toBe(true)
  })

  it('gibt false zurück, wenn die Media Query initial nicht zutrifft', () => {
    mockMatchMedia(false)
    const matches = useMediaQuery('(max-width: 767px)')
    expect(matches.value).toBe(false)
  })

  it('aktualisiert den Wert, wenn sich die Media Query ändert', async () => {
    const { fireChange } = mockMatchMedia(false)
    const matches = useMediaQuery('(max-width: 767px)')
    expect(matches.value).toBe(false)

    fireChange(true)
    expect(matches.value).toBe(true)

    fireChange(false)
    expect(matches.value).toBe(false)
  })
})

describe('useIsMobile', () => {
  beforeEach(() => {
    mockMatchMedia(false)
  })

  it('nutzt den Tablet-Breakpoint (max-width: 767px) als Mobile-Grenze', () => {
    useIsMobile()
    expect(window.matchMedia).toHaveBeenCalledWith('(max-width: 767px)')
  })

  it('ist true, wenn der Viewport schmaler als der Tablet-Breakpoint ist', () => {
    mockMatchMedia(true)
    const isMobile = useIsMobile()
    expect(isMobile.value).toBe(true)
  })

  it('ist false, wenn der Viewport ab Tablet-Breite liegt', () => {
    mockMatchMedia(false)
    const isMobile = useIsMobile()
    expect(isMobile.value).toBe(false)
  })
})
