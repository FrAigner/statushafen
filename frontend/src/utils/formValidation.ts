// Required-field check on submit for forms with `novalidate`.
//
// All forms carry `novalidate`, because otherwise Chromium and Firefox show the
// native validation message ("Please fill out this field.") as a tooltip as soon
// as one hovers over an empty required field. That collides with the help
// tooltips (v-help). But `novalidate` also switches off the check on submit.
// This guard brings it back: it listens in the capture phase on the document,
// so it runs before every @submit handler of the views, shows the usual
// browser hints for invalid fields (reportValidity) and stops the event, so
// the view does not save.
// Forms without `novalidate` are checked by the browser itself and stay untouched.
// Forms with `data-self-validates` check their required fields themselves with
// their own translated message (pattern:
// AdminNotificationTemplates) and stay untouched as well.

function guardInvalidSubmit(event: Event): void {
  const form = event.target as HTMLFormElement
  if (!form.noValidate || 'selfValidates' in form.dataset || form.checkValidity()) return
  event.preventDefault()
  event.stopImmediatePropagation()
  form.reportValidity()
}

export function installFormValidationGuard(target: Pick<EventTarget, 'addEventListener' | 'removeEventListener'> = document): () => void {
  target.addEventListener('submit', guardInvalidSubmit, true)
  return () => target.removeEventListener('submit', guardInvalidSubmit, true)
}
