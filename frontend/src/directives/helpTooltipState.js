import { reactive, shallowRef } from 'vue'

// gemeinsamer Zustand aller Hilfe-Tooltips (v-help) und der
// Tooltip-Ebene (HelpTooltipLayer.vue). Es ist höchstens ein Tooltip offen.

// Erst nach so langem Verweilen auf Feld/Label bzw. Button erscheint die
// Hilfe. Kurzes Überfahren beim normalen Arbeiten zeigt damit nichts.
export const HELP_SHOW_DELAY_MS = 800
// Nach dem Verlassen bleibt der Tooltip so lange offen, dass man den Zeiger
// hineinbewegen kann (WCAG 1.4.13: „hoverable“).
export const HELP_HIDE_DELAY_MS = 150
// Abstand zum Element und zum Viewport-Rand.
export const HELP_GAP = 6
export const HELP_VIEWPORT_MARGIN = 8

// Schlüssel -> Anzahl der Elemente, die ihn gerade benutzen. Die Ebene
// rendert für jeden Schlüssel einen (versteckten) Tooltip, auf den
// aria-describedby der Elemente zeigt.
export const helpRegistry = reactive(new Map())

// Offener Tooltip: { key, anchor } oder null.
export const activeHelp = shallowRef(null)

let showTimer = null
let pendingAnchor = null
let hideTimer = null

export function scheduleShow(key, anchor) {
  clearTimeout(showTimer)
  pendingAnchor = anchor
  showTimer = setTimeout(() => {
    pendingAnchor = null
    clearTimeout(hideTimer)
    activeHelp.value = { key, anchor }
  }, HELP_SHOW_DELAY_MS)
}

// Bricht ein noch nicht erschienenes Öffnen für dieses Element ab.
export function cancelShow(anchor) {
  if (pendingAnchor !== anchor) return
  clearTimeout(showTimer)
  pendingAnchor = null
}

export function scheduleHide() {
  clearTimeout(hideTimer)
  hideTimer = setTimeout(hideHelp, HELP_HIDE_DELAY_MS)
}

export function cancelHide() {
  clearTimeout(hideTimer)
}

// Schließt sofort und verwirft ein ausstehendes Öffnen.
export function hideHelp() {
  clearTimeout(showTimer)
  clearTimeout(hideTimer)
  pendingAnchor = null
  activeHelp.value = null
}

// Schließt, falls der offene Tooltip zu diesem Element gehört.
export function hideHelpFor(anchor) {
  cancelShow(anchor)
  if (activeHelp.value?.anchor === anchor) hideHelp()
}

// Position (position: fixed) unter dem Element, sonst darüber, horizontal
// mittig und in den Viewport geklemmt. Passt der Tooltip weder unter noch
// über das Element, steht er unten.
export function tooltipPosition(rect, size, viewport) {
  const centered = rect.left + rect.width / 2 - size.width / 2
  const left = Math.max(HELP_VIEWPORT_MARGIN, Math.min(centered, viewport.width - size.width - HELP_VIEWPORT_MARGIN))
  const below = rect.bottom + HELP_GAP
  const above = rect.top - HELP_GAP - size.height
  const fitsBelow = below + size.height <= viewport.height - HELP_VIEWPORT_MARGIN
  const top = fitsBelow || above < HELP_VIEWPORT_MARGIN ? below : above
  return { top: `${top}px`, left: `${left}px` }
}
