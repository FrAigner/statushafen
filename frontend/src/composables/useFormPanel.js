// Richtung C, Betriebsseiten: Fokus und Pflichtfeld-Fehler für das
// Formular in der Seitenkarte (FormPanel). Vorher stand dasselbe Muster
// (focusForm, nameError + Fokus aufs fehlerhafte Feld) in AdminComponents.vue;
// die Betriebsseiten teilen es sich hier.
import { nextTick, ref } from 'vue'

export function useFormPanel() {
  const formEl = ref(null)
  // Fehlertext je Feldschlüssel; leer = kein Fehler.
  const errors = ref({})

  // Nach dem Öffnen: Formular unter 860 px in den Sichtbereich holen, Fokus
  // aufs erste Feld.
  const focusForm = async () => {
    await nextTick()
    formEl.value?.closest('aside')?.scrollIntoView?.({ block: 'nearest' })
    formEl.value?.querySelector('input, textarea, select')?.focus({ preventScroll: true })
  }

  // Fokus aufs erste Feld mit aria-invalid (für Seiten mit eigener Prüfung).
  const focusInvalid = async () => {
    await nextTick()
    formEl.value?.querySelector('[aria-invalid="true"]')?.focus()
  }

  const clearErrors = () => { errors.value = {} }
  const clearError = (key) => {
    if (errors.value[key]) errors.value = { ...errors.value, [key]: '' }
  }

  const isBlank = (value) => Array.isArray(value) ? value.length === 0 : !String(value ?? '').trim()

  // fields: [{ key, value, message }]. Fehlende Pflichtfelder bekommen ihren
  // Text am Feld, der Fokus springt aufs erste fehlerhafte Feld.
  const validateRequired = async (fields) => {
    const failed = fields.filter(f => isBlank(f.value))
    errors.value = Object.fromEntries(failed.map(f => [f.key, f.message]))
    if (failed.length === 0) return true
    await focusInvalid()
    return false
  }

  return { formEl, errors, focusForm, focusInvalid, clearErrors, clearError, validateRequired }
}
