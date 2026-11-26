import { ref, computed } from 'vue'
import type { Ref } from 'vue'
import type { ApiFetch } from './useApi'
import type { EntityTranslation } from '../types/api'

/** ID of the translated entity or of a translation row (route params arrive as strings). */
export type EntityId = number | string

export function createTranslationLoader<T extends EntityTranslation = EntityTranslation>(endpoint: (id: EntityId) => string) {
  return () => {
    const translations = ref([]) as Ref<T[]>
    const loading = ref(false)
    const error = ref<string | null>(null)

    const loadTranslations = async (apiFetch: ApiFetch, entityId: EntityId) => {
      loading.value = true
      error.value = null
      try {
        translations.value = await apiFetch<T[]>(endpoint(entityId))
      } catch (err) {
        error.value = (err as Error).message
        translations.value = []
      } finally {
        loading.value = false
      }
    }

    const getTranslation = (languageCode: string): T | undefined => {
      return translations.value.find(t => t.LanguageCode === languageCode)
    }

    const translationsByLanguage = computed(() => {
      const map: Record<string, T> = {}
      for (const trans of translations.value) {
        map[trans.LanguageCode] = trans
      }
      return map
    })

    return {
      translations,
      loading,
      error,
      loadTranslations,
      getTranslation,
      translationsByLanguage
    }
  }
}
