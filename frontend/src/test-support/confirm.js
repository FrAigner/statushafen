// Test-Helfer für den gemeinsamen Bestätigungsdialog (ersetzt vi.stubGlobal('confirm', ...)).
//   const confirm = stubConfirm(true)          // Dialog antwortet "Bestätigt"
//   expect(confirm).toHaveBeenCalledWith('Text der Rückfrage')   // Mock sieht nur den Text
//   confirm.lastOptions                                          // volle Optionen (Titel, Beschriftung, danger)
// Die Stellen rufen den Dialog dadurch ohne Layout-Einbindung auf; stubConfirm wird nach jedem Test
// in vitestSetup.js zurückgesetzt.
import { vi } from 'vitest'
import { setConfirmHandler } from '../composables/useConfirm'

export function stubConfirm(answer) {
  const fn = vi.fn(() => answer)
  setConfirmHandler((opts) => {
    fn.lastOptions = opts
    return fn(opts.message)
  })
  return fn
}

export function resetConfirm() {
  setConfirmHandler(null)
}
