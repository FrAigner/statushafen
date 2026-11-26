import { reactive, shallowRef } from 'vue'

// Shared state of all help tooltips (v-help) and the tooltip layer
// (HelpTooltipLayer.vue). At most one tooltip is open.

/** The open tooltip: its help key and the element it belongs to. */
export interface ActiveHelp {
  key: string
  anchor: HTMLElement
}

export interface TooltipPosition {
  top: string
  left: string
}

// Only after resting this long on the field/label or button does the help
// appear. Brief passes during normal work therefore show nothing.
export const HELP_SHOW_DELAY_MS = 800
// After leaving, the tooltip stays open long enough to move the pointer
// into it (WCAG 1.4.13: "hoverable").
export const HELP_HIDE_DELAY_MS = 150
// Distance to the element and to the viewport edge.
export const HELP_GAP = 6
export const HELP_VIEWPORT_MARGIN = 8

// Key -> number of elements currently using it. The layer renders one
// (hidden) tooltip per key, which the elements' aria-describedby points to.
export const helpRegistry = reactive(new Map<string, number>())

// Open tooltip: { key, anchor } or null.
export const activeHelp = shallowRef<ActiveHelp | null>(null)

let showTimer: ReturnType<typeof setTimeout> | undefined
let pendingAnchor: HTMLElement | null = null
let hideTimer: ReturnType<typeof setTimeout> | undefined

export function scheduleShow(key: string, anchor: HTMLElement): void {
  clearTimeout(showTimer)
  pendingAnchor = anchor
  showTimer = setTimeout(() => {
    pendingAnchor = null
    clearTimeout(hideTimer)
    activeHelp.value = { key, anchor }
  }, HELP_SHOW_DELAY_MS)
}

// Cancels an opening for this element that has not appeared yet.
export function cancelShow(anchor: HTMLElement): void {
  if (pendingAnchor !== anchor) return
  clearTimeout(showTimer)
  pendingAnchor = null
}

export function scheduleHide(): void {
  clearTimeout(hideTimer)
  hideTimer = setTimeout(hideHelp, HELP_HIDE_DELAY_MS)
}

export function cancelHide(): void {
  clearTimeout(hideTimer)
}

// Closes immediately and discards a pending opening.
export function hideHelp(): void {
  clearTimeout(showTimer)
  clearTimeout(hideTimer)
  pendingAnchor = null
  activeHelp.value = null
}

// Closes if the open tooltip belongs to this element.
export function hideHelpFor(anchor: HTMLElement): void {
  cancelShow(anchor)
  if (activeHelp.value?.anchor === anchor) hideHelp()
}

// Position (position: fixed) below the element, otherwise above it, horizontally
// centred and clamped into the viewport. If the tooltip fits neither below nor
// above the element, it goes below.
export function tooltipPosition(
  rect: Pick<DOMRect, 'left' | 'width' | 'top' | 'bottom'>,
  size: { width: number; height: number },
  viewport: { width: number; height: number },
): TooltipPosition {
  const centered = rect.left + rect.width / 2 - size.width / 2
  const left = Math.max(HELP_VIEWPORT_MARGIN, Math.min(centered, viewport.width - size.width - HELP_VIEWPORT_MARGIN))
  const below = rect.bottom + HELP_GAP
  const above = rect.top - HELP_GAP - size.height
  const fitsBelow = below + size.height <= viewport.height - HELP_VIEWPORT_MARGIN
  const top = fitsBelow || above < HELP_VIEWPORT_MARGIN ? below : above
  return { top: `${top}px`, left: `${left}px` }
}
