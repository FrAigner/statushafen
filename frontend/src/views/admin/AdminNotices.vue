<script setup>
import { ref, computed, watch } from 'vue'
import { OnyxButton, OnyxCheckbox } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useResourcePerms } from '../../composables/useResourcePerms'
import { useTenant } from '../../composables/useTenant'
import { toLocalInputValue, formatDateTimeShort } from '../../utils/datetime'
import { useSaveAction, useDeleteAction } from '../../composables/useAdminCrudActions'
import { useFormTranslations } from '../../composables/useFormTranslations'
import FormLanguageSwitch from '../../components/FormLanguageSwitch.vue'
import FormLanguageDone from '../../components/FormLanguageDone.vue'
import { useFormPanel } from '../../composables/useFormPanel'
import { useListQuery } from '../../composables/useListQuery'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import AppIcon from '../../components/AppIcon.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import SegmentControl from '../../components/SegmentControl.vue'
import FormPanel from '../../components/FormPanel.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import RowMenu from '../../components/RowMenu.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import RichTextEditor from '../../components/RichTextEditor.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import ListState from '../../components/ListState.vue'
import { noticeBadge } from '../../utils/badgeStatus'

// General Notices: seitenweite Ankündigungen, unabhängig von
// Incidents/Wartungsfenstern. Struktur/Styling analog zu AdminIncidents.vue.

const { apiFetch } = useApi()
// Rechte dieses Tabs je Aktion. Ohne Ändern öffnet sich ein bestehender
// Eintrag schreibgeschützt (formReadOnly), ohne Löschen entfällt der Eintrag im Zeilenmenü.
const { canCreate, canUpdate, canDelete } = useResourcePerms('notices')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()
const breadcrumbs = usePageCrumbs(() => t('notices.title'))

const formData       = ref({})

// Sprachumschalter über Titel und Nachricht (weitere Sprachen der Seite).
// Übersetzungen folgen dem Recht „Ändern“ des Tabs.
const trans = useFormTranslations({
  apiFetch,
  urls: { list: (id) => `/api/notices/${id}/translations/all`, save: (id, code) => `/api/notices/${id}/translations/${code}` },
  formData,
  canEdit: canUpdate,
})
const { showSwitcher, onDefaultLang, titleModel, contentModel: messageModel, active: activeLang } = trans

const items          = ref([])
const isEditing      = ref(false)
const currentEditId  = ref(null)
const loading        = ref(false)
const loadFailed     = ref(false)
const alertMsg       = ref('')
const alertType      = ref('warning')
const isSaving       = ref(false)
const deletingId     = ref(null)

// Richtung C: Titel des Formulars nennt die Ankündigung, nicht die ID.
const formTitle = computed(() => currentEditId.value
  ? t('notices.editTitle', { name: formData.value.Title })
  : t('notices.createTitle'))

const levelOptions = computed(() => ['info', 'warning', 'critical']
  .map(value => ({ label: noticeBadge(value, t).label, value })))

// Stufe 4b: ResponsiveTable/dataTable, Stufe als StatusBadge.
const columns = computed(() => [
  { key: 'Title', label: t('notices.tableTitle') },
  { key: 'Level', label: t('notices.tableLevel') },
  { key: 'IsActive', label: t('notices.tableActive') },
  { key: 'Window', label: t('notices.tableWindow') },
  { key: 'CreatedAt', label: t('notices.tableCreated') },
])

/* ── Liste: Suche und Zustandsfilter im Kartenkopf ── */
const { query, filter, rows, searching } = useListQuery(items, {
  textOf: item => `${item.Title} ${item.Message || ''}`,
  predicates: { active: item => item.IsActive, inactive: item => !item.IsActive },
})
const filterOptions = computed(() => [
  { value: 'all', label: t('common.all') },
  { value: 'active', label: t('notices.filterActive') },
  { value: 'inactive', label: t('notices.filterInactive') },
])
const activeCount = computed(() => items.value.filter(item => item.IsActive).length)
const subtitle = computed(() => t('notices.subtitle', {
  count: t('notices.countNotices', { n: items.value.length }, items.value.length),
  active: activeCount.value,
}))

/* ── Data loading ── */
const loadData = async () => {
  loadFailed.value = false
  loading.value = true
  try {
    items.value = await apiFetch(`/api/pages/${activeTenantId.value}/notices/all`) ?? []
  } catch (err) {
    loadFailed.value = true
    showAlert(t('common.loadError') + err.message, 'error')
  } finally {
    loading.value = false
  }
  await trans.load(activeTenantId.value)
}

watch(activeTenantId, (newId) => {
  if (newId) {
    loadData()
  }
}, { immediate: true })

/* ── CRUD helpers ── */
// vor jeder neuen Aktion die Meldung des vorigen Schritts entfernen.
const clearAlert = () => { alertMsg.value = '' }
const showAlert = (msg, type = 'warning') => {
  alertMsg.value  = msg
  alertType.value = type
}

const openNew = () => {
  formData.value  = { Level: 'info', IsActive: true, Collapsed: false, StartAt: '', EndAt: '', NotifySubscribers: false }
  currentEditId.value = null
  trans.reset()
  clearErrors()
  isEditing.value = true
  focusForm()
}

const openEdit = (item) => {
  formData.value  = {
    ...item,
    StartAt: toLocalInputValue(item.StartAt),
    EndAt: toLocalInputValue(item.EndAt),
  }
  currentEditId.value = item.ID
  clearErrors()
  isEditing.value = true
  focusForm()
  if (canUpdate.value) trans.loadFor(item.ID)
  else trans.reset()
}

const cancelEdit = () => {
  isEditing.value = false
  formData.value  = {}
  currentEditId.value = null
  trans.reset()
  clearErrors()
}

const saveAction = useSaveAction({ apiFetch, isSaving, reload: loadData, onStart: clearAlert, onError: (err) => showAlert(t('common.saveError') + err.message, 'error') })
const deleteAction = useDeleteAction({ apiFetch, deletingId, reload: loadData, onStart: clearAlert, onDeleted: () => showAlert(t('adminFeedback.deleted'), 'success'), onError: (err) => showAlert(t('common.deleteError') + err.message, 'error') })

const saveItem = async () => {
  // Pflichtfelder: Fehler am Feld statt eines Serverfehlers, Fokus aufs erste.
  const valid = await validateRequired([
    { key: 'Title', value: formData.value.Title, message: t('patterns.fieldRequired', { field: t('notices.fieldTitle') }) },
    { key: 'Message', value: formData.value.Message, message: t('patterns.fieldRequired', { field: t('notices.fieldMessage') }) },
  ])
  if (!valid) {
    activeLang.value = '' // die Pflichtfelder gehören zur Standardsprache
    return
  }
  if (!trans.checkComplete(showAlert)) return
  const isNew = currentEditId.value === null
  const url   = isNew
    ? `/api/pages/${activeTenantId.value}/notices`
    : `/api/pages/${activeTenantId.value}/notices/${currentEditId.value}`
  const payload = { ...formData.value }
  payload.StartAt = formData.value.StartAt ? new Date(formData.value.StartAt).toISOString() : null
  payload.EndAt   = formData.value.EndAt   ? new Date(formData.value.EndAt).toISOString()   : null
  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved) => {
    // Neue ID merken, bevor Übersetzungen folgen: scheitert eine, legt ein zweiter Versuch keinen zweiten Eintrag an.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value)
    showAlert(isNew ? t('notices.created') : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id, name) =>
  deleteAction(`/api/pages/${activeTenantId.value}/notices/${id}`, id, t('notices.confirmDelete', { name }))

const formatDate = (d) => formatDateTimeShort(d, locale.value)

// Zeilenmenü: „Übersetzungen“ ist ein Link, der Rest meldet seinen key.
const menuItems = (item) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  { key: 'translations', label: t('translations.manageLink'), icon: 'globe', to: `/admin/notices/${item.ID}/translations` },
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === item.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === item.ID }] : []),
]
const onMenuSelect = (item, key) => {
  if (key === 'edit') openEdit(item)
  else deleteItem(item.ID, item.Title) // „Übersetzungen“ ist ein Link und meldet nichts
}
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('notices.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="canCreate" #actions>
        <OnyxButton :label="t('notices.newButton')" @click="openNew" />
      </template>
    </PageHeader>

    <!-- Richtung C: Liste und Formular nebeneinander (unter 860 px untereinander). -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('notices.cardTitle')" class="split__main">
        <template #tools>
          <SegmentControl v-model="filter" :options="filterOptions" :label="t('notices.filterLabel')" />
          <SearchBox v-model="query" :label="t('notices.searchLabel')" :placeholder="t('notices.searchPlaceholder')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('patterns.searchResult', { n: rows.length }, rows.length) }}</p>

        <ResponsiveTable v-if="rows.length" data-table :columns="columns" :items="rows" row-key="ID">
          <template #cell-Title="{ item }">
            <strong class="component-name__title">{{ item.Title }}</strong>
            <small class="cell-sub">{{ item.Message }}</small>
          </template>
          <template #cell-Level="{ item }">
            <StatusBadge :status="noticeBadge(item.Level, t).status" :label="noticeBadge(item.Level, t).label" />
          </template>
          <template #cell-IsActive="{ item }">
            <StatusBadge :status="item.IsActive ? 'operational' : null" :label="item.IsActive ? t('notices.filterActive') : t('notices.filterInactive')" />
          </template>
          <template #cell-Window="{ item }">
            <span class="cell-changed">
              <template v-if="!item.StartAt && !item.EndAt">{{ t('notices.unlimited') }}</template>
              <template v-else>{{ formatDate(item.StartAt) }} – {{ formatDate(item.EndAt) }}</template>
            </span>
          </template>
          <template #cell-CreatedAt="{ item }"><RelativeTime :value="item.CreatedAt" /></template>
          <template #actions="{ item }">
            <!-- Übersetzungen nur noch über das Zeilenmenü. -->
            <RowMenu :label="t('patterns.menuLabel', { name: item.Title })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="items.length" :title="t('notices.noResults')" :text="t('notices.noResultsText')" />
        <ListState v-else :loading="loading" :error="loadFailed" :count="items.length" :empty-text="t('notices.empty')" icon="info" />
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
          <FormLanguageSwitch :trans="trans" />
          <fieldset class="form-lock" :disabled="formReadOnly">

          <FormField v-slot="f" required :label="t('notices.fieldTitle')" :error="onDefaultLang ? errors.Title || null : null">
            <input :id="f.id" v-model="titleModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('notices.titlePlaceholder')" @input="clearError('Title')">
          </FormField>

          <FormField v-slot="f" required :label="t('notices.fieldMessage')" :error="onDefaultLang ? errors.Message || null : null">
            <RichTextEditor :id="f.id" v-model="messageModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('notices.messagePlaceholder')" @input="clearError('Message')" />
          </FormField>

          <FormLanguageDone :trans="trans" />

          <FormField v-slot="f" required :label="t('notices.fieldLevel')">
            <select :id="f.id" v-model="formData.Level" :required="f.required">
              <option v-for="opt in levelOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </FormField>

          <div class="notice-options">
            <OnyxCheckbox v-model="formData.IsActive" :label="t('notices.fieldActive')" />
            <OnyxCheckbox v-model="formData.Collapsed" :label="t('notices.fieldCollapsed')" />
            <!-- Opt-in-Mailversand. Beim Anlegen (bzw. Erst-Aktivierung
                 einer inaktiven Notice) wird eine Mail an alle Email-Subscriber
                 dieser Statusseite geschickt. -->
            <OnyxCheckbox v-model="formData.NotifySubscribers" :label="t('notices.fieldNotifySubscribers')" />
          </div>

          <div class="form-grid">
            <FormField v-slot="f" :label="t('notices.fieldStart')">
              <input :id="f.id" v-model="formData.StartAt" type="datetime-local">
            </FormField>
            <FormField v-slot="f" :label="t('notices.fieldEnd')">
              <input :id="f.id" v-model="formData.EndAt" type="datetime-local">
            </FormField>
          </div>

          </fieldset>
          <div class="form-actions">
            <OnyxButton :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton v-if="!formReadOnly" :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>

  </div>
</template>

<style scoped>
.notice-options {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  margin-bottom: var(--sp-4);
}
.cell-sub {
  display: block;
  max-inline-size: 300px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-2);
}
</style>
