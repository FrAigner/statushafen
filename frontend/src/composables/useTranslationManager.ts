import { ref } from 'vue'
import type { ApiFetch } from './useApi'
import type { EntityId } from './useTranslationFactory'

export function useTranslationManager(loadTranslationsFn: (apiFetch: ApiFetch, entityId: EntityId) => unknown) {
  const alertMsg = ref('')
  const alertType = ref('info')
  const isEditing = ref(false)
  const formData = ref<Record<string, unknown>>({})
  const isSaving = ref(false)
  const confirmingId = ref<EntityId | null>(null)
  const deletingId = ref<EntityId | null>(null)

  const showAlert = (msg: string, type = 'info') => {
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

  const reloadTranslations = async (apiFetch: ApiFetch, entityId: EntityId) => {
    try {
      await loadTranslationsFn(apiFetch, entityId)
    } catch (err) {
      showAlert('Fehler beim Neuladen: ' + (err as Error).message, 'error')
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
