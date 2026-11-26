// Brings a setting into view after the quick search navigated to its page:
// waits until the target (or its fallback) is rendered, scrolls it to the
// middle of the viewport (clear of the sticky .admin-topbar), focuses the
// control and highlights the surrounding field for a moment.
export const HIGHLIGHT_CLASS = 'quick-search-highlight'
const HIGHLIGHT_MS = 2500
const CONTAINER = '.form-field, .checkbox-row, .form-group, .setting, section, fieldset'
const FOCUSABLE = 'input:not([type="hidden"]), select, textarea, button, [tabindex]:not([tabindex="-1"])'

function findTarget(id: string, fallbackId?: string): HTMLElement | null {
  return document.getElementById(id) ?? (fallbackId ? document.getElementById(fallbackId) : null)
}

function waitForTarget(id: string, fallbackId: string | undefined, timeout: number, interval: number): Promise<HTMLElement | null> {
  return new Promise(resolve => {
    const started = Date.now()
    const poll = () => {
      const el = findTarget(id, fallbackId)
      if (el) resolve(el)
      else if (Date.now() - started >= timeout) resolve(null)
      else setTimeout(poll, interval)
    }
    poll()
  })
}

function focusTarget(el: HTMLElement): HTMLElement {
  if (el.matches(FOCUSABLE)) return el
  const inner = el.querySelector<HTMLElement>(FOCUSABLE)
  if (inner) return inner
  el.setAttribute('tabindex', '-1')
  return el
}

const prefersReducedMotion = () => typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches

export interface RevealSettingOptions {
  fallbackId?: string
  timeout?: number
  interval?: number
}

export async function revealSetting(id: string, { fallbackId, timeout = 4000, interval = 50 }: RevealSettingOptions = {}): Promise<HTMLElement | null> {
  const el = await waitForTarget(id, fallbackId, timeout, interval)
  if (!el) return null
  const container = el.matches(CONTAINER) ? el : (el.closest<HTMLElement>(CONTAINER) ?? el)
  el.scrollIntoView({ block: 'center', behavior: prefersReducedMotion() ? 'auto' : 'smooth' })
  focusTarget(el).focus({ preventScroll: true })
  container.classList.add(HIGHLIGHT_CLASS)
  setTimeout(() => container.classList.remove(HIGHLIGHT_CLASS), HIGHLIGHT_MS)
  return el
}
