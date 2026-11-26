import { ref, onMounted, computed } from 'vue'
import type { Ref } from 'vue'
import { useApi } from './useApi'
import { useLanguage } from './useLanguage'
import type { EntityTranslation } from '../types/api'

// Known gap: useLanguage returns no currentLanguageCode (only the test mock of LocalizedComponent
// provides it), so outside tests currentTranslation would throw. The only user, LocalizedComponent.vue,
// is not mounted anywhere. The type migration keeps the behaviour unchanged and only makes the
// assumption explicit.
type LanguageWithCode = ReturnType<typeof useLanguage> & { currentLanguageCode: Readonly<Ref<string>> }

export function useComponentLocalization(entity: { ID: number }, endpoint: (id: number) => string) {
  const { apiFetch } = useApi()
  const { currentLanguageCode } = useLanguage() as LanguageWithCode

  const translations = ref<EntityTranslation[]>([])
  const loading = ref(false)

  onMounted(async () => {
    loading.value = true
    try {
      translations.value = await apiFetch<EntityTranslation[]>(endpoint(entity.ID))
    } catch (err) {
      console.error('Failed to load translations:', err)
    } finally {
      loading.value = false
    }
  })

  const getTranslation = (languageCode: string) => {
    return translations.value.find(t => t.LanguageCode === languageCode)
  }

  const currentTranslation = computed(() => {
    return getTranslation(currentLanguageCode.value)
  })

  return {
    translations,
    loading,
    getTranslation,
    currentTranslation
  }
}
