// Gemeinsame Save/Delete-Handler für die Admin-CRUD-Views (Components,
// Incidents, IncidentTemplates, Maintenance, Notices, Roles, StatusPages,
// Users) - der isSaving/try/catch/finally- bzw. confirm/deletingId/try/
// catch/finally-Rahmen war in allen acht Views nahezu identisch dupliziert
// (Code-Duplikation). Payload-Aufbau
// und Erfolgsverhalten bleiben bewusst in den Views, da die dort variieren.

// onStart räumt die Meldung des vorigen Schritts ab, bevor eine neue Aktion läuft (sonst
// blieb z. B. „Wartungsfenster erstellt.“ nach dem Löschen stehen); onDeleted meldet das Löschen.
import { confirmDialog } from './useConfirm'

export function useSaveAction({ apiFetch, isSaving, reload, onError, onStart }) {
  return async (url, method, payload, onSuccess) => {
    onStart?.()
    isSaving.value = true
    try {
      // Antwort und Wartezeit für Aufrufer, die danach noch Folgeaufrufe brauchen (z. B. Übersetzungen mit der neuen ID).
      const saved = await apiFetch(url, { method, body: JSON.stringify(payload) })
      if (onSuccess) await onSuccess(saved)
      await reload()
    } catch (err) {
      onError(err)
    } finally {
      isSaving.value = false
    }
  }
}

export function useDeleteAction({ apiFetch, deletingId, reload, onError, onStart, onDeleted }) {
  return async (url, id, confirmMessage) => {
    if (!(await confirmDialog({ message: confirmMessage }))) return
    onStart?.()
    deletingId.value = id
    try {
      await apiFetch(url, { method: 'DELETE' })
      onDeleted?.()
      await reload()
    } catch (err) {
      onError(err)
    } finally {
      deletingId.value = null
    }
  }
}
