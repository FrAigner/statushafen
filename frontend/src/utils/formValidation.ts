// Pflichtfeld-Prüfung beim Absenden für Formulare mit `novalidate`.
//
// Alle Formulare tragen `novalidate`, weil Chromium und Firefox sonst die
// native Validierungsmeldung („Fülle dieses Feld aus.“) schon beim Hover über
// ein leeres Pflichtfeld als Tooltip zeigen. Das kollidiert mit den
// Hilfe-Tooltips (v-help).`novalidate` schaltet aber auch die Prüfung beim
// Absenden ab. Dieser Wächter holt sie zurück: Er lauscht in der
// Capture-Phase am document, läuft damit vor jedem @submit-Handler der Views,
// zeigt bei ungültigen Feldern die üblichen Browser-Hinweise
// (reportValidity) und stoppt das Event, sodass die View nicht speichert.
// Formulare ohne `novalidate` prüft der Browser selbst, die bleiben unberührt.
// Formulare mit `data-self-validates` prüfen ihre Pflichtfelder selbst mit
// eigener, übersetzter Meldung (Muster,
// AdminNotificationTemplates) und bleiben ebenfalls unberührt.

function guardInvalidSubmit(event) {
  const form = event.target
  if (!form.noValidate || 'selfValidates' in form.dataset || form.checkValidity()) return
  event.preventDefault()
  event.stopImmediatePropagation()
  form.reportValidity()
}

export function installFormValidationGuard(target = document) {
  target.addEventListener('submit', guardInvalidSubmit, true)
  return () => target.removeEventListener('submit', guardInvalidSubmit, true)
}
