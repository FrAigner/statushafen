import { effectScope, h, render, watch } from 'vue'
import type { EffectScope, ObjectDirective } from 'vue'
import HelpTooltipLayer from '../components/HelpTooltipLayer.vue'
import { helpDomId, helpTextFor } from '../composables/useHelpText'
import { helpTooltipsEnabled } from '../composables/useHelpTooltips'
import {
  activeHelp, helpRegistry, hideHelp, hideHelpFor, scheduleHide, scheduleShow, cancelHide, cancelShow,
} from './helpTooltipState'

// v-help="'area.field'" attaches the help (what / effect /
// example from help.<area>.<field>) directly to a field or a button,
// without its own ⓘ icon (feedback from the first pilot: too many icons).
//
// Behaviour:
// - Mouse (and pen) rests HELP_SHOW_DELAY_MS on the element: the help
//   appears. Leaving earlier cancels. Leaving afterwards closes with a
//   delay; moving into the tooltip keeps it open.
// - Keyboard: focus via Tab opens after the same delay; the focus
//   stays where it is. Focus via mouse/click does not open.
// - Never in the way: typing (keydown/input) in the element and clicking/pressing
//   close the help or prevent it from opening; after a click it stays
//   off until the pointer has left the element. Escape closes.
// - Touch: no help (decision in the PR). Without hover there would only be
//   tapping (disturbs every interaction) or long-pressing, which in fields
//   triggers text selection or the context menu. Pointer events with
//   pointerType "touch" are therefore ignored.
// - aria-describedby: points from the operable element to the tooltip while
//   the help is switched on. If v-help sits on a sit-onyx component, the
//   element is its root node (label + field); the fields or buttons inside
//   are described. A group (role="group") is described itself.
// - Switched off (useHelpTooltips): no listeners, no aria-describedby,
//   no tooltip layer in the DOM.

const CONTROLS = 'input:not([type="hidden"]), select, textarea, button'
const STATE = Symbol('v-help')

type Handlers = Record<string, (event: Event) => void>

interface HelpState {
  el: HelpElement
  key: string
  attached: boolean
  suppressed: boolean
  targets: Element[]
  handlers: Handlers
  scope: EffectScope
}

type HelpElement = HTMLElement & { [STATE]?: HelpState }

/* ── Layer and global listeners, only while elements have help ── */

let attachedCount = 0
let layerHost: HTMLDivElement | null = null
let keyboardModality = false

function onGlobalKeydown(event: KeyboardEvent): void {
  keyboardModality = true
  if (event.key === 'Escape') hideHelp()
}

function onGlobalPointerdown(event: PointerEvent): void {
  keyboardModality = false
  const active = activeHelp.value
  if (active && !document.getElementById(helpDomId(active.key))?.contains(event.target as Node | null)) hideHelp()
}

function ensureLayer(): void {
  if (layerHost?.isConnected) return
  if (layerHost) render(null, layerHost)
  layerHost = document.createElement('div')
  layerHost.className = 'help-tooltip-layer'
  document.body.appendChild(layerHost)
  render(h(HelpTooltipLayer), layerHost)
}

function acquire(key: string): void {
  helpRegistry.set(key, (helpRegistry.get(key) ?? 0) + 1)
  if (attachedCount++ === 0) {
    document.addEventListener('keydown', onGlobalKeydown, true)
    document.addEventListener('pointerdown', onGlobalPointerdown, true)
  }
  ensureLayer()
}

function release(key: string): void {
  const left = (helpRegistry.get(key) as number) - 1
  if (left > 0) helpRegistry.set(key, left)
  else helpRegistry.delete(key)
  if (--attachedCount > 0) return
  document.removeEventListener('keydown', onGlobalKeydown, true)
  document.removeEventListener('pointerdown', onGlobalPointerdown, true)
  hideHelp()
  render(null, layerHost as HTMLDivElement)
  ;(layerHost as HTMLDivElement).remove()
  layerHost = null
}

/* ── aria-describedby ── */

function describedTargets(el: HTMLElement): Element[] {
  if (el.matches(CONTROLS) || el.getAttribute('role') === 'group') return [el]
  const inner = [...el.querySelectorAll(CONTROLS)]
  return inner.length ? inner : [el]
}

function setDescribedBy(target: Element, id: string, on: boolean): void {
  const ids = (target.getAttribute('aria-describedby') || '').split(/\s+/).filter(token => token && token !== id)
  if (on) ids.push(id)
  if (ids.length) target.setAttribute('aria-describedby', ids.join(' '))
  else target.removeAttribute('aria-describedby')
}

function describe(state: HelpState, on: boolean): void {
  const id = helpDomId(state.key)
  state.targets.forEach(target => setDescribedBy(target, id, false))
  state.targets = on ? describedTargets(state.el) : []
  state.targets.forEach(target => setDescribedBy(target, id, true))
}

/* ── Listeners on the element ── */

function elementHandlers(state: HelpState): Handlers {
  const { el } = state
  const close = () => hideHelpFor(el)
  return {
    pointerenter(event: Event) {
      if ((event as PointerEvent).pointerType === 'touch' || state.suppressed) return
      scheduleShow(state.key, el)
      if (activeHelp.value?.anchor === el) cancelHide()
    },
    pointerleave(event: Event) {
      if ((event as PointerEvent).pointerType === 'touch') return
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
    focusout(event: Event) {
      if (!el.contains((event as FocusEvent).relatedTarget as Node | null)) close()
    },
    keydown(event: Event) {
      if ((event as KeyboardEvent).key !== 'Escape') close()
    },
    input: close,
  }
}

function attach(state: HelpState): void {
  if (state.attached || !helpTextFor(state.key)) return
  state.attached = true
  state.handlers = elementHandlers(state)
  for (const [type, handler] of Object.entries(state.handlers)) state.el.addEventListener(type, handler)
  acquire(state.key)
  describe(state, true)
}

function detach(state: HelpState): void {
  if (!state.attached) return
  state.attached = false
  for (const [type, handler] of Object.entries(state.handlers)) state.el.removeEventListener(type, handler)
  hideHelpFor(state.el)
  describe(state, false)
  release(state.key)
}

export const vHelp: ObjectDirective<HelpElement, string> = {
  mounted(el, binding) {
    const state: HelpState = { el, key: binding.value, attached: false, suppressed: false, targets: [], handlers: {}, scope: effectScope(true) }
    el[STATE] = state
    state.scope.run(() => {
      watch(helpTooltipsEnabled, on => (on ? attach(state) : detach(state)), { immediate: true })
    })
  },
  updated(el, binding) {
    const state = el[STATE] as HelpState
    if (binding.value !== state.key) {
      detach(state)
      state.key = binding.value
      if (helpTooltipsEnabled.value) attach(state)
    } else if (state.attached) {
      // Components like OnyxSelect can recreate their field.
      describe(state, true)
    }
  },
  beforeUnmount(el) {
    const state = el[STATE] as HelpState
    state.scope.stop()
    detach(state)
  },
}

export default vHelp
