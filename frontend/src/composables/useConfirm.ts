// A shared confirmation dialog instead of the browser's native dialog.
//
// Usage: `if (!(await confirmDialog({ message, title?, confirmLabel?, danger? }))) return`.
// The state lives at module level; exactly one <ConfirmDialog /> (in App.vue) shows it.
// A second call while a dialog is open answers the first one with "cancelled"
// (never two dialogs on top of each other, never a hanging promise).
import { reactive, readonly } from 'vue'

export interface ConfirmOptions {
  message?: string
  title?: string
  confirmLabel?: string
  danger?: boolean
}

export interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  danger: boolean
  opener: Element | null
  themeClass: 'theme-admin' | 'theme-public'
}

/** Test replacement for the dialog (src/test-support/confirm): answers with a fixed value. */
export type ConfirmHandler = (options: ConfirmOptions & Required<Omit<ConfirmOptions, 'message'>>) => boolean | Promise<boolean>

const state = reactive<ConfirmState>({
  open: false,
  title: '',
  message: '',
  confirmLabel: '',
  danger: true,
  // The element that triggered the dialog; gets the focus back on close.
  opener: null,
  // Theme class (theme-admin/theme-public) of the triggering element: the dialog is teleported to <body>
  // and would otherwise sit outside the layout's token layer.
  themeClass: 'theme-public',
})
let resolver: ((answer: boolean) => void) | null = null
let testHandler: ConfirmHandler | null = null

function settle(answer: boolean) {
  const done = resolver
  resolver = null
  state.open = false
  done?.(answer)
}

export function confirmDialog({ message, title = '', confirmLabel = '', danger = true }: ConfirmOptions = {}): Promise<boolean> {
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

export function answerConfirm(answer: boolean): Element | null {
  const opener = state.opener
  settle(answer)
  state.opener = null
  return opener
}

export function useConfirmState() {
  return readonly(state)
}

// Tests only (src/test-support/confirm.ts): replaces the dialog with a fixed answer.
export function setConfirmHandler(fn: ConfirmHandler | null) {
  testHandler = fn
}
