import { ref, computed } from 'vue'

export function createTranslationLoader(endpoint) {
  return () => {
    const translations = ref([])
    const loading = ref(false)
    const error = ref(null)

    const loadTranslations = async (apiFetch, entityId) => {
      loading.value = true
      error.value = null
      try {
        translations.value = await apiFetch(endpoint(entityId))
      } catch (err) {
        error.value = err.message
        translations.value = []
      } finally {
        loading.value = false
      }
    }

    const getTranslation = (languageCode) => {
      return translations.value.find(t => t.LanguageCode === languageCode)
    }

    const translationsByLanguage = computed(() => {
      const map = {}
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
