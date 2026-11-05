import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from './useApi'
import { useTenant } from './useTenant'
import { useResourcePerms } from './useResourcePerms'
import { useContentLanguages } from './useContentLanguages'
import { confirmDialog } from './useConfirm'

// Gemeinsame CRUD-Logik für die drei Admin-Übersetzungs-Views (Incident/
// Component/GeneralNotice, Stage 5) - die Views waren bis auf
// Endpoint-Segment, Content-Feldname (message/description) und
// Entity-Titelfeld (Title/Name) identisch, was nach der i18n-Umstellung
// (Folge-PR) das SonarCloud-Duplication-Gate auf "New Code"
// gerissen hat. Extrahiert nach demselben Muster wie
// useAdminTranslationFactory.js für die reinen API-Aufrufe.
//
// Für Komponenten und Ankündigungen gibt es im Backend keinen
// Einzel-GET (`/api/pages/:id/components/:id` bzw. `…/notices/:id` liefern
// 404). Statt dafür zwei neue Endpunkte zu bauen, die nur diese View braucht,
// lädt `findInList` die vorhandene Liste der Seite (Komponenten als Baum mit
// `Children`, Ankündigungen über `notices/all`) und sucht den Eintrag darin.
// Vorfälle haben einen Einzel-GET und nutzen ihn weiter.
export function findEntityById(list, id) {
  for (const entry of list || []) {
    if (String(entry.ID) === String(id)) return entry
    const nested = findEntityById(entry.Children, id)
    if (nested) return nested
  }
  return null
}

export function useAdminTranslationsPanel({ entityId, apiSegment, findInList = false, contentField, translationsApi, resource }) {
  const { apiFetch } = useApi()
  const { activeTenantId } = useTenant()
  const { catalog, loadCatalog } = useContentLanguages()
  const { t } = useI18n()
  const { canCreate, canUpdate, canDelete } = useResourcePerms(resource)
  // Anlegen, Bestätigen und Löschen prüft das Backend mit
  // <ressource>:create, :update (Bestätigen) und :delete (siehe main.go); ohne
  // das Recht blenden die Views die Knöpfe aus statt 403 zu kassieren. Die
  // Ressource ist ein eigener Parameter statt aus apiSegment abgeleitet, weil das
  // Segment der Ankündigungen 'notices/all' lautet.
  const { translations, loadTranslations, createTranslation, confirmTranslation, deleteTranslation } = translationsApi

  const entity = ref(null)
  const alertMsg = ref('')
  const alertType = ref('info')
  const isEditing = ref(false)
  const formData = ref({})
  const isSaving = ref(false)
  const confirmingId = ref(null)
  const deletingId = ref(null)

  const showAlert = (msg, type = 'info') => { alertMsg.value = msg; alertType.value = type }

  const loadData = async () => {
    try {
      const base = `/api/pages/${activeTenantId.value}/${apiSegment}`
      entity.value = findInList
        ? findEntityById(await apiFetch(base), entityId.value)
        : await apiFetch(`${base}/${entityId.value}`)
      await loadTranslations(apiFetch, parseInt(entityId.value))
      await loadCatalog()
    } catch (err) {
      showAlert(t('common.loadError') + err.message, 'error')
    }
  }

  onMounted(loadData)
  watch(activeTenantId, loadData)

  const usedLanguages = () => new Set(translations.value.map(trans => trans.LanguageCode))
  const availableLanguages = () => catalog.value.filter(l => !usedLanguages().has(l.Code))

  const openNew = () => { formData.value = { language_code: '', title: '', [contentField]: '' }; isEditing.value = true }
  const cancelEdit = () => { isEditing.value = false; formData.value = {} }

  const saveTranslation = async () => {
    if (!formData.value.language_code || !formData.value.title || !formData.value[contentField]) {
      showAlert(t('translations.requiredFields'), 'warning')
      return
    }
    isSaving.value = true
    try {
      await createTranslation(apiFetch, parseInt(entityId.value), {
        language_code: formData.value.language_code,
        title: formData.value.title,
        [contentField]: formData.value[contentField],
        source: 'manual'
      })
      await loadTranslations(apiFetch, parseInt(entityId.value))
      cancelEdit()
      showAlert(t('translations.created'), 'success')
    } catch (err) {
      showAlert(t('common.saveError') + err.message, 'error')
    } finally {
      isSaving.value = false
    }
  }

  const handleConfirm = async (transId) => {
    confirmingId.value = transId
    try {
      await confirmTranslation(apiFetch, transId)
      await loadTranslations(apiFetch, parseInt(entityId.value))
      showAlert(t('translations.confirmed'), 'success')
    } catch (err) {
      showAlert(t('translations.confirmError') + err.message, 'error')
    } finally {
      confirmingId.value = null
    }
  }

  const handleDelete = async (transId) => {
    if (!(await confirmDialog({ message: t('translations.confirmDelete') }))) return
    deletingId.value = transId
    try {
      await deleteTranslation(apiFetch, transId)
      await loadTranslations(apiFetch, parseInt(entityId.value))
      showAlert(t('translations.deleted'), 'success')
    } catch (err) {
      showAlert(t('common.deleteError') + err.message, 'error')
    } finally {
      deletingId.value = null
    }
  }

  return {
    entity, canCreate, canUpdate, canDelete, alertMsg, alertType, isEditing, formData, isSaving, confirmingId, deletingId,
    translations, availableLanguages,
    openNew, cancelEdit, saveTranslation, handleConfirm, handleDelete,
  }
}
