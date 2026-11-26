import { ref } from 'vue'

export function createAdminTranslationLoader(endpoints) {
  return () => {
    const translations = ref([])

    // Die Admin-Tabelle lädt über `listAll` (Entwürfe und
    // Bestätigte). `list` ist die öffentliche Route, die nur bestätigte
    // Übersetzungen liefert - darüber verschwanden frisch angelegte Entwürfe
    // nach dem Speichern, "Bestätigen" erschien nie. Angelegt wird weiter
    // per POST auf `list`.
    const loadTranslations = async (apiFetch, entityId) => {
      const result = await apiFetch(endpoints.listAll(entityId))
      translations.value = result || []
    }

    const createTranslation = async (apiFetch, entityId, payload) => {
      return await apiFetch(endpoints.list(entityId), {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }

    const confirmTranslation = async (apiFetch, translationId) => {
      return await apiFetch(endpoints.confirm(translationId), {
        method: 'PATCH'
      })
    }

    const deleteTranslation = async (apiFetch, translationId) => {
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
