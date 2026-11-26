import { watch, onScopeDispose, toValue } from 'vue'

/*
  Closes a popup (menu, panel, popover) when the user clicks or taps outside
  it, or presses Escape.

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
export function useDismissable(open, inside, onDismiss, { escape = true } = {}) {
  const elements = () => inside
    .map(source => toValue(source))
    .map(value => value?.$el ?? value)
    .filter(Boolean)

  const onPointerdown = (event) => {
    if (!elements().some(el => el.contains(event.target))) onDismiss('outside')
  }

  const onKeydown = (event) => {
    if (event.key !== 'Escape') return
    event.preventDefault()
    onDismiss('escape')
  }

  let attached = false
  const detach = () => {
    if (!attached) return
    attached = false
    document.removeEventListener('pointerdown', onPointerdown, true)
    if (escape) document.removeEventListener('keydown', onKeydown, false)
  }
  // The watcher below only fires on a change of the boolean, so attach never
  // runs twice in a row.
  const attach = () => {
    attached = true
    document.addEventListener('pointerdown', onPointerdown, true)
    if (escape) document.addEventListener('keydown', onKeydown, false)
  }

  watch(() => !!toValue(open), isOpen => (isOpen ? attach() : detach()), { immediate: true })
  onScopeDispose(detach)
}
