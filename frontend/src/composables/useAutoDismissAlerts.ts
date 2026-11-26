import { onMounted, onBeforeUnmount } from 'vue'
import type { Ref } from 'vue'

// Success messages ("saved") in the admin area disappear by themselves after a few seconds
// (operator request 31.10.2026). Errors and warnings stay until they are closed.
// Central instead of in each of the roughly 20 views: observes the content area and, after the
// delay, clicks the message's close cross, so the view clears its state (alertMsg) itself.
export const ALERT_AUTO_DISMISS_MS = 6000

// nodeType 1 = ELEMENT_NODE.
const isElement = (node: Node): node is Element => node.nodeType === 1

export function useAutoDismissAlerts(containerRef: Readonly<Ref<Element | null | undefined>>, ms = ALERT_AUTO_DISMISS_MS) {
  // Message element -> running timer. The views render ONE message node and only patch class and
  // text (error -> success); so every change is re-evaluated: success arms the delay (a second
  // success message within the window restarts it), anything else clears the timer.
  const timers = new Map<Element, ReturnType<typeof setTimeout>>()
  let observer: MutationObserver | null = null

  const disarm = (alert: Element) => {
    clearTimeout(timers.get(alert))
    timers.delete(alert)
  }

  const refresh = (alert: Element) => {
    disarm(alert)
    if (!alert.isConnected || !alert.matches('.alert-success')) return
    timers.set(alert, setTimeout(() => {
      timers.delete(alert)
      alert.querySelector<HTMLElement>('.alert-close')?.click()
    }, ms))
  }

  const scan = (root: Node) => {
    if (!isElement(root)) return
    if (root.matches('.alert')) refresh(root)
    root.querySelectorAll('.alert').forEach(refresh)
  }

  onMounted(() => {
    const el = containerRef.value
    if (!el) return
    scan(el)
    observer = new MutationObserver((records) => {
      for (const r of records) {
        r.addedNodes.forEach(scan)
        // Class, text or child change on an existing message.
        const target = isElement(r.target) ? r.target : r.target.parentElement
        const alert = target?.closest('.alert')
        if (alert) refresh(alert)
      }
      for (const alert of [...timers.keys()]) if (!alert.isConnected) disarm(alert)
    })
    observer.observe(el, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'], characterData: true })
  })

  onBeforeUnmount(() => {
    observer?.disconnect()
    timers.forEach(clearTimeout)
    timers.clear()
  })
}
