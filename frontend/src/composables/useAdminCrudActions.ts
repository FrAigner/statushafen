// Shared save/delete handlers for the admin CRUD views (Components,
// Incidents, IncidentTemplates, Maintenance, Notices, Roles, StatusPages,
// Users): the isSaving/try/catch/finally and confirm/deletingId/try/
// catch/finally frames were duplicated almost identically in all eight views
// (code duplication). Building the payload and the success behaviour
// deliberately stay in the views, since they vary there.

// onStart clears the message of the previous step before a new action runs (otherwise
// e.g. "Maintenance window created." stayed after deleting); onDeleted reports the deletion.
import { confirmDialog } from './useConfirm'
import type { Ref } from 'vue'
import type { ApiFetch } from './useApi'
import type { EntityId } from './useTranslationFactory'
import type { ApiError } from '../types/api'

interface CrudActionOptions {
  apiFetch: ApiFetch
  reload: () => unknown
  /** Receives what apiFetch (or onSuccess/reload) threw; apiFetch throws ApiError. */
  onError: (err: ApiError) => void
  onStart?: () => void
}

export interface SaveActionOptions extends CrudActionOptions {
  isSaving: Ref<boolean>
}

export interface DeleteActionOptions extends CrudActionOptions {
  deletingId: Ref<EntityId | null>
  onDeleted?: () => void
}

export function useSaveAction({ apiFetch, isSaving, reload, onError, onStart }: SaveActionOptions) {
  return async <T = unknown>(url: string, method: string, payload: unknown, onSuccess?: (saved: T) => unknown): Promise<void> => {
    onStart?.()
    isSaving.value = true
    try {
      // Response and waiting for callers that need follow-up calls afterwards (e.g. translations with the new ID).
      const saved = await apiFetch<T>(url, { method, body: JSON.stringify(payload) })
      if (onSuccess) await onSuccess(saved)
      await reload()
    } catch (err) {
      onError(err as ApiError)
    } finally {
      isSaving.value = false
    }
  }
}

export function useDeleteAction({ apiFetch, deletingId, reload, onError, onStart, onDeleted }: DeleteActionOptions) {
  return async (url: string, id: EntityId, confirmMessage: string): Promise<void> => {
    if (!(await confirmDialog({ message: confirmMessage }))) return
    onStart?.()
    deletingId.value = id
    try {
      await apiFetch(url, { method: 'DELETE' })
      onDeleted?.()
      await reload()
    } catch (err) {
      onError(err as ApiError)
    } finally {
      deletingId.value = null
    }
  }
}
