// Direction C, operations pages: focus and required-field errors for the
// form in the page card (FormPanel). Before, the same pattern (focusForm,
// nameError + focus on the faulty field) lived in AdminComponents.vue; the
// operations pages share it here.
import { nextTick, ref } from 'vue'

/** Required field for validateRequired: blank value -> message at the field. */
export interface RequiredField {
  key: string
  value: unknown
  message: string
}

export function useFormPanel() {
  const formEl = ref<HTMLElement | null>(null)
  // Error text per field key; empty = no error.
  const errors = ref<Record<string, string>>({})

  // After opening: scroll the form into view below 860 px, focus the first
  // field.
  const focusForm = async () => {
    await nextTick()
    formEl.value?.closest('aside')?.scrollIntoView?.({ block: 'nearest' })
    formEl.value?.querySelector<HTMLElement>('input, textarea, select')?.focus({ preventScroll: true })
  }

  // Focus the first field with aria-invalid (for pages with their own validation).
  const focusInvalid = async () => {
    await nextTick()
    formEl.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  }

  const clearErrors = () => { errors.value = {} }
  const clearError = (key: string) => {
    if (errors.value[key]) errors.value = { ...errors.value, [key]: '' }
  }

  const isBlank = (value: unknown) => Array.isArray(value) ? value.length === 0 : !String(value ?? '').trim()

  // fields: [{ key, value, message }]. Missing required fields get their
  // text at the field, the focus jumps to the first faulty field.
  const validateRequired = async (fields: RequiredField[]): Promise<boolean> => {
    const failed = fields.filter(f => isBlank(f.value))
    errors.value = Object.fromEntries(failed.map(f => [f.key, f.message]))
    if (failed.length === 0) return true
    await focusInvalid()
    return false
  }

  return { formEl, errors, focusForm, focusInvalid, clearErrors, clearError, validateRequired }
}
