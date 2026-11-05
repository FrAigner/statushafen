import { effectScope, h, render, watch } from 'vue'
import HelpTooltipLayer from '../components/HelpTooltipLayer.vue'
import { helpDomId, helpTextFor } from '../composables/useHelpText'
import { helpTooltipsEnabled } from '../composables/useHelpTooltips'
import {
  activeHelp, helpRegistry, hideHelp, hideHelpFor, scheduleHide, scheduleShow, cancelHide, cancelShow,
} from './helpTooltipState'

// v-help="'bereich.feld'" hängt die Hilfe (Was / Auswirkung /
// Beispiel aus help.<bereich>.<feld>) direkt an ein Feld oder einen Button,
// ohne eigenes ⓘ-Symbol (Rückmeldung zum ersten Pilot: zu viele Symbole).
//
// Verhalten:
// - Maus (und Stift) bleibt HELP_SHOW_DELAY_MS auf dem Element: Hilfe
//   erscheint. Verlassen vorher bricht ab. Danach Verlassen schließt
//   verzögert, Hineinfahren in den Tooltip hält ihn offen.
// - Tastatur: Fokus per Tab öffnet nach derselben Verzögerung, der Fokus
//   bleibt, wo er ist. Fokus per Maus/Klick öffnet nicht.
// - Nie im Weg: Tippen (keydown/input) im Element und Klicken/Drücken
//   schließen die Hilfe bzw. verhindern das Öffnen; nach einem Klick bleibt
//   sie aus, bis der Zeiger das Element verlassen hat. Escape schließt.
// - Touch: keine Hilfe (Entscheidung im PR). Ohne Hover gäbe es nur
//   Antippen (stört jede Bedienung) oder langes Drücken, das in Feldern die
//   Textauswahl bzw. das Kontextmenü auslöst. Pointer-Events mit
//   pointerType "touch" werden deshalb ignoriert.
// - aria-describedby: zeigt am bedienbaren Element auf den Tooltip, solange
//   die Hilfe eingeschaltet ist. Sitzt v-help auf einer sit-onyx-Komponente,
//   ist das Element ihr Wurzelknoten (Label + Feld); beschrieben werden die
//   Felder bzw. Buttons darin. Eine Gruppe (role="group") wird selbst
//   beschrieben.
// - Ausgeschaltet (useHelpTooltips): keine Listener, kein aria-describedby,
//   keine Tooltip-Ebene im DOM.

const CONTROLS = 'input:not([type="hidden"]), select, textarea, button'
const STATE = Symbol('v-help')

/* ── Ebene und globale Listener, nur solange Elemente Hilfe haben ── */

let attachedCount = 0
let layerHost = null
let keyboardModality = false

function onGlobalKeydown(event) {
  keyboardModality = true
  if (event.key === 'Escape') hideHelp()
}

function onGlobalPointerdown(event) {
  keyboardModality = false
  const active = activeHelp.value
  if (active && !document.getElementById(helpDomId(active.key))?.contains(event.target)) hideHelp()
}

function ensureLayer() {
  if (layerHost?.isConnected) return
  if (layerHost) render(null, layerHost)
  layerHost = document.createElement('div')
  layerHost.className = 'help-tooltip-layer'
  document.body.appendChild(layerHost)
  render(h(HelpTooltipLayer), layerHost)
}

function acquire(key) {
  helpRegistry.set(key, (helpRegistry.get(key) ?? 0) + 1)
  if (attachedCount++ === 0) {
    document.addEventListener('keydown', onGlobalKeydown, true)
    document.addEventListener('pointerdown', onGlobalPointerdown, true)
  }
  ensureLayer()
}

function release(key) {
  const left = helpRegistry.get(key) - 1
  if (left > 0) helpRegistry.set(key, left)
  else helpRegistry.delete(key)
  if (--attachedCount > 0) return
  document.removeEventListener('keydown', onGlobalKeydown, true)
  document.removeEventListener('pointerdown', onGlobalPointerdown, true)
  hideHelp()
  render(null, layerHost)
  layerHost.remove()
  layerHost = null
}

/* ── aria-describedby ── */

function describedTargets(el) {
  if (el.matches(CONTROLS) || el.getAttribute('role') === 'group') return [el]
  const inner = [...el.querySelectorAll(CONTROLS)]
  return inner.length ? inner : [el]
}

function setDescribedBy(target, id, on) {
  const ids = (target.getAttribute('aria-describedby') || '').split(/\s+/).filter(token => token && token !== id)
  if (on) ids.push(id)
  if (ids.length) target.setAttribute('aria-describedby', ids.join(' '))
  else target.removeAttribute('aria-describedby')
}

function describe(state, on) {
  const id = helpDomId(state.key)
  state.targets.forEach(target => setDescribedBy(target, id, false))
  state.targets = on ? describedTargets(state.el) : []
  state.targets.forEach(target => setDescribedBy(target, id, true))
}

/* ── Listener am Element ── */

function elementHandlers(state) {
  const { el } = state
  const close = () => hideHelpFor(el)
  return {
    pointerenter(event) {
      if (event.pointerType === 'touch' || state.suppressed) return
      scheduleShow(state.key, el)
      if (activeHelp.value?.anchor === el) cancelHide()
    },
    pointerleave(event) {
      if (event.pointerType === 'touch') return
      state.suppressed = false
      cancelShow(el)
      if (activeHelp.value?.anchor === el) scheduleHide()
    },
    pointerdown() {
      state.suppressed = true
      close()
    },
    focusin() {
      if (keyboardModality) scheduleShow(state.key, el)
    },
    focusout(event) {
      if (!el.contains(event.relatedTarget)) close()
    },
    keydown(event) {
      if (event.key !== 'Escape') close()
    },
    input: close,
  }
}

function attach(state) {
  if (state.attached || !helpTextFor(state.key)) return
  state.attached = true
  state.handlers = elementHandlers(state)
  for (const [type, handler] of Object.entries(state.handlers)) state.el.addEventListener(type, handler)
  acquire(state.key)
  describe(state, true)
}

function detach(state) {
  if (!state.attached) return
  state.attached = false
  for (const [type, handler] of Object.entries(state.handlers)) state.el.removeEventListener(type, handler)
  hideHelpFor(state.el)
  describe(state, false)
  release(state.key)
}

export const vHelp = {
  mounted(el, binding) {
    const state = { el, key: binding.value, attached: false, suppressed: false, targets: [], handlers: {}, scope: effectScope(true) }
    el[STATE] = state
    state.scope.run(() => {
      watch(helpTooltipsEnabled, on => (on ? attach(state) : detach(state)), { immediate: true })
    })
  },
  updated(el, binding) {
    const state = el[STATE]
    if (binding.value !== state.key) {
      detach(state)
      state.key = binding.value
      if (helpTooltipsEnabled.value) attach(state)
    } else if (state.attached) {
      // Komponenten wie OnyxSelect können ihr Feld neu erzeugen.
      describe(state, true)
    }
  },
  beforeUnmount(el) {
    const state = el[STATE]
    state.scope.stop()
    detach(state)
  },
}

export default vHelp
