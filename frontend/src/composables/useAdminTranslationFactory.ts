import { ref } from 'vue'
import type { Ref } from 'vue'
import type { ApiFetch } from './useApi'
import type { EntityId } from './useTranslationFactory'
import type { EntityTranslation } from '../types/api'

/** Routes of an entity's translations (see e.g. useIncidentTranslations). */
export interface AdminTranslationEndpoints {
  /** Public list (confirmed only); new translations are POSTed here. */
  list: (entityId: EntityId) => string
  /** Drafts and confirmed translations, for the admin table. */
  listAll: (entityId: EntityId) => string
  confirm: (translationId: EntityId) => string
  delete: (translationId: EntityId) => string
}

export function createAdminTranslationLoader<T extends EntityTranslation = EntityTranslation>(endpoints: AdminTranslationEndpoints) {
  return () => {
    const translations = ref([]) as Ref<T[]>

    // The admin table loads via `listAll` (drafts and confirmed ones). `list`
    // is the public route that only returns confirmed translations; through it,
    // freshly created drafts disappeared after saving and "Confirm" never
    // appeared. Creating still POSTs to `list`.
    const loadTranslations = async (apiFetch: ApiFetch, entityId: EntityId) => {
      const result = await apiFetch<T[] | null>(endpoints.listAll(entityId))
      translations.value = result || []
    }

    const createTranslation = async (apiFetch: ApiFetch, entityId: EntityId, payload: Record<string, unknown>) => {
      return await apiFetch<T>(endpoints.list(entityId), {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }

    const confirmTranslation = async (apiFetch: ApiFetch, translationId: EntityId) => {
      return await apiFetch<T>(endpoints.confirm(translationId), {
        method: 'PATCH'
      })
    }

    const deleteTranslation = async (apiFetch: ApiFetch, translationId: EntityId) => {
      return await apiFetch(endpoints.delete(translationId), {
        method: 'DELETE'
      })
    }

    return {
      translations,
      loadTranslations,
      createTranslation,
      confirmTranslation,
      deleteTranslation
    }
  }
}

/** What createAdminTranslationLoader's composables return (input of useAdminTranslationsPanel). */
export type AdminTranslationsApi<T extends EntityTranslation = EntityTranslation> = ReturnType<ReturnType<typeof createAdminTranslationLoader<T>>>
