import { onMounted, onBeforeUnmount } from 'vue'

// Erfolgsmeldungen („gespeichert“) im Admin-Bereich verschwinden nach einigen Sekunden von selbst
// (Betreiber-Wunsch 31.10.2026). Fehler und Warnungen bleiben stehen, bis man sie schließt.
// Zentral statt in jeder der rund 20 Ansichten: beobachtet den Inhaltsbereich und klickt nach der
// Frist auf das Schließen-Kreuz der Meldung, damit die Ansicht ihren Zustand (alertMsg) selbst leert.
export const ALERT_AUTO_DISMISS_MS = 6000

export function useAutoDismissAlerts(containerRef, ms = ALERT_AUTO_DISMISS_MS) {
  // Meldungselement -> laufender Timer. Die Ansichten rendern EINEN Meldungsknoten und patchen nur Klasse
  // und Text (Fehler -> Erfolg); deshalb wird bei jeder Änderung neu bewertet: Erfolg bewaffnet die Frist
  // (eine zweite Erfolgsmeldung im Fenster startet sie neu), alles andere löscht den Timer.
  const timers = new Map()
  let observer = null

  const disarm = (alert) => {
    clearTimeout(timers.get(alert))
    timers.delete(alert)
  }

  const refresh = (alert) => {
    disarm(alert)
    if (!alert.isConnected || !alert.matches('.alert-success')) return
    timers.set(alert, setTimeout(() => {
      timers.delete(alert)
      alert.querySelector('.alert-close')?.click()
    }, ms))
  }

  const scan = (root) => {
    if (root.nodeType !== 1) return
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
        // Klassen-, Text- oder Kinderwechsel an einer bestehenden Meldung.
        const target = r.target.nodeType === 1 ? r.target : r.target.parentElement
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
