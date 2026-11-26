// Ein gemeinsamer Bestätigungsdialog statt des nativen Browser-Dialogs.
//
// Aufruf: `if (!(await confirmDialog({ message, title?, confirmLabel?, danger? }))) return`.
// Der Zustand liegt auf Modulebene, genau eine <ConfirmDialog /> (in App.vue) zeigt ihn an.
// Ein zweiter Aufruf, während ein Dialog offen ist, beantwortet den ersten mit "abgebrochen"
// (nie zwei Dialoge übereinander, nie ein hängendes Promise).
import { reactive, readonly } from 'vue'

const state = reactive({
  open: false,
  title: '',
  message: '',
  confirmLabel: '',
  danger: true,
  // Das Element, das den Dialog ausgelöst hat; bekommt beim Schließen den Fokus zurück.
  opener: null,
  // Themeklasse (theme-admin/theme-public) des auslösenden Elements: der Dialog hängt per Teleport am <body>
  // und läge sonst außerhalb der Token-Ebene des Layouts.
  themeClass: 'theme-public',
})
let resolver = null
let testHandler = null

function settle(answer) {
  const done = resolver
  resolver = null
  state.open = false
  done?.(answer)
}

export function confirmDialog({ message, title = '', confirmLabel = '', danger = true } = {}) {
  if (testHandler) return Promise.resolve(testHandler({ message, title, confirmLabel, danger }))
  if (resolver) settle(false)
  return new Promise((resolve) => {
    resolver = resolve
    const opener = typeof document === 'undefined' ? null : document.activeElement
    Object.assign(state, {
      open: true, title, message, confirmLabel, danger, opener,
      themeClass: opener?.closest?.('.theme-admin') ? 'theme-admin' : 'theme-public',
    })
  })
}

export function answerConfirm(answer) {
  const opener = state.opener
  settle(answer)
  state.opener = null
  return opener
}

export function useConfirmState() {
  return readonly(state)
}

// Nur für Tests (src/test-support/confirm.js): ersetzt den Dialog durch eine feste Antwort.
export function setConfirmHandler(fn) {
  testHandler = fn
}
