import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { revealSetting, HIGHLIGHT_CLASS } from './revealSetting'

describe('revealSetting', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    document.body.innerHTML = ''
    Element.prototype.scrollIntoView = vi.fn()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('waits for the element, scrolls it into view, focuses and highlights its field', async () => {
    const done = revealSetting('page-x')
    setTimeout(() => {
      document.body.innerHTML = '<div class="form-field"><label for="page-x">X</label><input id="page-x"></div>'
    }, 120)
    await vi.advanceTimersByTimeAsync(200)
    const el = await done
    const input = document.getElementById('page-x')!
    expect(el).toBe(input)
    expect(input.scrollIntoView).toHaveBeenCalledWith(expect.objectContaining({ block: 'center' }))
    expect(document.activeElement).toBe(input)
    expect(input.closest('.form-field')!.classList.contains(HIGHLIGHT_CLASS)).toBe(true)

    await vi.advanceTimersByTimeAsync(3000)
    expect(input.closest('.form-field')!.classList.contains(HIGHLIGHT_CLASS)).toBe(false)
  })

  it('focuses the first control inside a section target', async () => {
    document.body.innerHTML = '<section id="setting-a" class="setting"><h4>A</h4><input type="checkbox" id="cb"></section>'
    const done = revealSetting('setting-a')
    await vi.advanceTimersByTimeAsync(0)
    await done
    expect(document.activeElement!.id).toBe('cb')
    expect(document.getElementById('setting-a')!.classList.contains(HIGHLIGHT_CLASS)).toBe(true)
  })

  it('makes a target without controls focusable', async () => {
    document.body.innerHTML = '<section id="s"><h4>Nur Text</h4></section>'
    const done = revealSetting('s')
    await vi.advanceTimersByTimeAsync(0)
    await done
    const s = document.getElementById('s')!
    expect(s.getAttribute('tabindex')).toBe('-1')
    expect(document.activeElement).toBe(s)
  })

  it('uses the fallback target when only that one shows up', async () => {
    document.body.innerHTML = '<div class="checkbox-row"><input type="checkbox" id="isPrivate"><label for="isPrivate">P</label></div>'
    const done = revealSetting('page-autojoin-domains', { fallbackId: 'isPrivate' })
    await vi.advanceTimersByTimeAsync(0)
    expect((await done)!.id).toBe('isPrivate')
  })

  it('highlights the target itself when it has no field container, without smooth scrolling for reduced motion', async () => {
    const original = window.matchMedia
    window.matchMedia = vi.fn(() => ({ matches: true }) as MediaQueryList)
    try {
      document.body.innerHTML = '<div><input id="bare"></div>'
      const done = revealSetting('bare')
      await vi.advanceTimersByTimeAsync(0)
      const el = (await done)!
      expect(el.classList.contains(HIGHLIGHT_CLASS)).toBe(true)
      expect(el.scrollIntoView).toHaveBeenCalledWith({ block: 'center', behavior: 'auto' })
    } finally {
      window.matchMedia = original
    }
  })

  it('gives up quietly after the timeout', async () => {
    const done = revealSetting('missing', { timeout: 500 })
    await vi.advanceTimersByTimeAsync(600)
    expect(await done).toBeNull()
  })
})
