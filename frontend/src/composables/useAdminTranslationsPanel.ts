import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from './useApi'
import { useTenant } from './useTenant'
import { useResourcePerms } from './useResourcePerms'
import { useContentLanguages } from './useContentLanguages'
import { useTranslationEnabled } from './useTranslationEnabled'
import { confirmDialog } from './useConfirm'
import { useRouter } from 'vue-router'
import type { Ref } from 'vue'
import type { AdminTranslationsApi } from './useAdminTranslationFactory'
import type { EntityId } from './useTranslationFactory'

/** The translated entity as the panel shows it (incident, component or notice). */
export interface TranslatableEntity {
  ID: number
  Title?: string
  Name?: string
  Children?: TranslatableEntity[]
  [key: string]: unknown
}

export interface AdminTranslationsPanelOptions {
  entityId: Readonly<Ref<string | number>>
  apiSegment: string
  findInList?: boolean
  contentField: string
  translationsApi: AdminTranslationsApi
  resource: string
  overviewPath: string
}

// Shared CRUD logic for the three admin translation views (incident/
// component/general notice, stage 5). The views were identical except for
// the endpoint segment, the content field name (message/description) and the
// entity title field (Title/Name), which broke SonarCloud's duplication gate
// on "New Code" after the i18n migration (follow-up PR). Extracted following
// the same pattern as useAdminTranslationFactory for the plain API calls.
//
// For components and notices the backend has no single GET
// (`/api/pages/:id/components/:id` and `…/notices/:id` return 404). Instead
// of building two new endpoints that only this view needs, `findInList` loads
// the page's existing list (components as a tree with `Children`, notices via
// `notices/all`) and looks up the entry in it. Incidents have a single GET and
// keep using it.
export function findEntityById<T extends { ID: number | string; Children?: T[] | null }>(list: T[] | null | undefined, id: EntityId): T | null {
  for (const entry of list || []) {
    if (String(entry.ID) === String(id)) return entry
    const nested = findEntityById(entry.Children, id)
    if (nested) return nested
  }
  return null
}

export function useAdminTranslationsPanel({ entityId, apiSegment, findInList = false, contentField, translationsApi, resource, overviewPath }: AdminTranslationsPanelOptions) {
  const { apiFetch } = useApi()
  // Switched off translation: the page goes back to its overview (overviewPath).
  const { load: loadTranslationEnabled } = useTranslationEnabled()
  const router = useRouter()
  const { activeTenantId } = useTenant()
  const { catalog, loadCatalog } = useContentLanguages()
  const { t } = useI18n()
  const { canCreate, canUpdate, canDelete } = useResourcePerms(resource)
  // The backend checks creating, confirming and deleting with
  // <resource>:create, :update (confirm) and :delete (see main.go); without
  // the permission the views hide the buttons instead of collecting a 403. The
  // resource is a separate parameter instead of being derived from apiSegment,
  // because the notices' segment is 'notices/all'.
  const { translations, loadTranslations, createTranslation, confirmTranslation, deleteTranslation } = translationsApi

  const entity = ref<TranslatableEntity | null>(null)
  const alertMsg = ref('')
  const alertType = ref('info')
  const isEditing = ref(false)
  const formData = ref<Record<string, string>>({})
  const isSaving = ref(false)
  const confirmingId = ref<EntityId | null>(null)
  const deletingId = ref<EntityId | null>(null)

  const showAlert = (msg: string, type = 'info') => { alertMsg.value = msg; alertType.value = type }

  const loadData = async () => {
    if (!(await loadTranslationEnabled())) {
      router.replace(overviewPath)
      return
    }
    try {
      const base = `/api/pages/${activeTenantId.value}/${apiSegment}`
      entity.value = findInList
        ? findEntityById(await apiFetch<TranslatableEntity[] | null>(base), entityId.value)
        : await apiFetch<TranslatableEntity>(`${base}/${entityId.value}`)
      await loadTranslations(apiFetch, parseInt(String(entityId.value)))
      await loadCatalog()
    } catch (err) {
      showAlert(t('common.loadError') + (err as Error).message, 'error')
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
      await createTranslation(apiFetch, parseInt(String(entityId.value)), {
        language_code: formData.value.language_code,
        title: formData.value.title,
        [contentField]: formData.value[contentField],
        source: 'manual'
      })
      await loadTranslations(apiFetch, parseInt(String(entityId.value)))
      cancelEdit()
      showAlert(t('translations.created'), 'success')
    } catch (err) {
      showAlert(t('common.saveError') + (err as Error).message, 'error')
    } finally {
      isSaving.value = false
    }
  }

  const handleConfirm = async (transId: EntityId) => {
    confirmingId.value = transId
    try {
      await confirmTranslation(apiFetch, transId)
      await loadTranslations(apiFetch, parseInt(String(entityId.value)))
      showAlert(t('translations.confirmed'), 'success')
    } catch (err) {
      showAlert(t('translations.confirmError') + (err as Error).message, 'error')
    } finally {
      confirmingId.value = null
    }
  }

  const handleDelete = async (transId: EntityId) => {
    if (!(await confirmDialog({ message: t('translations.confirmDelete') }))) return
    deletingId.value = transId
    try {
      await deleteTranslation(apiFetch, transId)
      await loadTranslations(apiFetch, parseInt(String(entityId.value)))
      showAlert(t('translations.deleted'), 'success')
    } catch (err) {
      showAlert(t('common.deleteError') + (err as Error).message, 'error')
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
