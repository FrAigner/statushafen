import { ref, onMounted, computed } from 'vue'
import { useApi } from './useApi'
import { useLanguage } from './useLanguage'

export function useComponentLocalization(entity, endpoint) {
  const { apiFetch } = useApi()
  const { currentLanguageCode } = useLanguage()

  const translations = ref([])
  const loading = ref(false)

  onMounted(async () => {
    loading.value = true
    try {
      translations.value = await apiFetch(endpoint(entity.ID))
    } catch (err) {
      console.error('Failed to load translations:', err)
    } finally {
      loading.value = false
    }
  })

  const getTranslation = (languageCode) => {
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
