import { watch, onScopeDispose, toValue } from 'vue'
import type { MaybeRefOrGetter } from 'vue'

/** Why a popup closes. */
export type DismissReason = 'outside' | 'escape'

/** Something that counts as inside: an element, a component instance ($el) or nothing yet (unmounted ref). */
export type DismissableTarget = Element | { $el?: unknown } | null | undefined

/*
  Closes a popup (menu, panel, popover) when the user clicks or taps outside
  it, moves the focus outside it (e.g. Tab or opening another popup by
  keyboard), or presses Escape.

  - open:     ref/getter, listeners are only attached while it is true.
  - inside:   elements that count as "inside" (trigger button and panel):
              refs to elements or components, or getter functions.
  - onDismiss(reason): called with 'outside' or 'escape'; the caller closes
              the popup and decides whether to restore focus.
  - options.escape: false leaves Escape to the component (own key handling).

  pointerdown (capture phase) covers mouse, pen and touch, and fires before a
  click inside the page could stop propagation. iOS Safari does not deliver
  click events to the document for taps on non-interactive elements, so
  'click' would not be enough. Escape is handled in the bubble phase, so a
  nested dialog that stops propagation keeps its own Escape.
*/
export function useDismissable(
  open: MaybeRefOrGetter<unknown>,
  inside: MaybeRefOrGetter<DismissableTarget>[],
  onDismiss: (reason: DismissReason) => void,
  { escape = true }: { escape?: boolean } = {},
) {
  const elements = () => inside
    .map(source => toValue(source))
    .map(value => (value as { $el?: unknown } | null | undefined)?.$el ?? value)
    .filter((value): value is Node => Boolean(value))

  // Used for pointerdown and focusin: both carry the new target.
  const onOutside = (event: Event) => {
    if (!elements().some(el => el.contains(event.target as Node | null))) onDismiss('outside')
  }

  const onKeydown = (event: KeyboardEvent) => {
    if (event.key !== 'Escape') return
    event.preventDefault()
    onDismiss('escape')
  }

  let attached = false
  const detach = () => {
    if (!attached) return
    attached = false
    document.removeEventListener('pointerdown', onOutside, true)
    document.removeEventListener('focusin', onOutside, true)
    if (escape) document.removeEventListener('keydown', onKeydown, false)
  }
  // The watcher below only fires on a change of the boolean, so attach never
  // runs twice in a row.
  const attach = () => {
    attached = true
    document.addEventListener('pointerdown', onOutside, true)
    document.addEventListener('focusin', onOutside, true)
    if (escape) document.addEventListener('keydown', onKeydown, false)
  }

  watch(() => !!toValue(open), isOpen => (isOpen ? attach() : detach()), { immediate: true })
  onScopeDispose(detach)
}
