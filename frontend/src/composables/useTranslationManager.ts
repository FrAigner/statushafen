import { ref } from 'vue'

export function useTranslationManager(loadTranslationsFn) {
  const alertMsg = ref('')
  const alertType = ref('info')
  const isEditing = ref(false)
  const formData = ref({})
  const isSaving = ref(false)
  const confirmingId = ref(null)
  const deletingId = ref(null)

  const showAlert = (msg, type = 'info') => {
    alertMsg.value = msg
    alertType.value = type
  }

  const openNew = () => {
    isEditing.value = true
  }

  const cancelEdit = () => {
    isEditing.value = false
    formData.value = {}
  }

  const reloadTranslations = async (apiFetch, entityId) => {
    try {
      await loadTranslationsFn(apiFetch, entityId)
    } catch (err) {
      showAlert('Fehler beim Neuladen: ' + err.message, 'error')
    }
  }

  return {
    alertMsg,
    alertType,
    isEditing,
    formData,
    isSaving,
    confirmingId,
    deletingId,
    showAlert,
    openNew,
    cancelEdit,
    reloadTranslations
  }
}
