// Test helper for the shared confirmation dialog (replaces vi.stubGlobal('confirm', ...)).
//   const confirm = stubConfirm(true)          // the dialog answers "confirmed"
//   expect(confirm).toHaveBeenCalledWith('Text der Rückfrage')   // the mock only sees the text
//   confirm.lastOptions                                          // full options (title, label, danger)
// The call sites thereby open the dialog without the layout; stubConfirm is reset after every test
// in vitestSetup.ts.
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import { setConfirmHandler } from '../composables/useConfirm'
import type { ConfirmOptions } from '../composables/useConfirm'

export type ConfirmStub = Mock<(message?: string) => boolean> & { lastOptions?: ConfirmOptions }

export function stubConfirm(answer: boolean): ConfirmStub {
  const fn: ConfirmStub = vi.fn((_message?: string) => answer)
  setConfirmHandler((opts) => {
    fn.lastOptions = opts
    return fn(opts.message)
  })
  return fn
}

export function resetConfirm() {
  setConfirmHandler(null)
}
