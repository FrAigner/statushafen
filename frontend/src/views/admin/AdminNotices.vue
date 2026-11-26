<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { OnyxButton, OnyxCheckbox } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { vHelp } from '../../directives/vHelp'
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
import type { GeneralNotice } from '../../types/api'

// General notices: page-wide announcements, independent of incidents/maintenance
// windows. Structure/styling like AdminIncidents.vue.

const { apiFetch } = useApi()
// Permissions of this tab per action. Without update an existing entry opens
// read-only (formReadOnly); without delete the row menu entry is gone.
const { canCreate, canUpdate, canDelete } = useResourcePerms('notices')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()
const breadcrumbs = usePageCrumbs(() => t('notices.title'))

// Edit form; StartAt/EndAt hold datetime-local input values while editing.
const formData       = ref<Partial<GeneralNotice>>({})

// Language switcher above title and message (further languages of the page).
// Translations follow the tab's "update" permission.
const trans = useFormTranslations({
  apiFetch,
  urls: { list: (id) => `/api/notices/${id}/translations/all`, save: (id, code) => `/api/notices/${id}/translations/${code}` },
  formData,
  canEdit: canUpdate,
})
const { showSwitcher, onDefaultLang, titleModel, contentModel: messageModel, active: activeLang } = trans

const items          = ref<GeneralNotice[]>([])
const isEditing      = ref(false)
const currentEditId  = ref<number | null>(null)
const loading        = ref(false)
const loadFailed     = ref(false)
const alertMsg       = ref('')
const alertType      = ref('warning')
const isSaving       = ref(false)
const deletingId     = ref<number | null>(null)

// Direction C: the form title names the notice, not the ID.
const formTitle = computed(() => currentEditId.value
  ? t('notices.editTitle', { name: formData.value.Title })
  : t('notices.createTitle'))

const levelOptions = computed(() => ['info', 'warning', 'critical']
  .map(value => ({ label: noticeBadge(value, t).label, value })))

// Stage 4b: ResponsiveTable/dataTable, level as a StatusBadge.
const columns = computed(() => [
  { key: 'Title', label: t('notices.tableTitle') },
  { key: 'Level', label: t('notices.tableLevel') },
  { key: 'IsActive', label: t('notices.tableActive') },
  { key: 'Window', label: t('notices.tableWindow') },
  { key: 'CreatedAt', label: t('notices.tableCreated') },
])

/* ── List: search and state filter in the card header ── */
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
    showAlert(t('common.loadError') + (err as Error).message, 'error')
  } finally {
    loading.value = false
  }
  await trans.load(activeTenantId.value!)
}

watch(activeTenantId, (newId) => {
  if (newId) {
    loadData()
  }
}, { immediate: true })

/* ── CRUD helpers ── */
// remove the previous step's message before every new action.
const clearAlert = () => { alertMsg.value = '' }
const showAlert = (msg: string, type = 'warning') => {
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

const openEdit = (item: GeneralNotice) => {
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
  // Required fields: error at the field instead of a server error, focus on the first one.
  const valid = await validateRequired([
    { key: 'Title', value: formData.value.Title, message: t('patterns.fieldRequired', { field: t('notices.fieldTitle') }) },
    { key: 'Message', value: formData.value.Message, message: t('patterns.fieldRequired', { field: t('notices.fieldMessage') }) },
  ])
  if (!valid) {
    activeLang.value = '' // the required fields belong to the default language
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
  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved: { ID?: number } | null) => {
    // Remember the new ID before translations follow: if one fails, a second attempt does not create a second entry.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value!)
    showAlert(isNew ? t('notices.created') : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id: number, name: string) =>
  deleteAction(`/api/pages/${activeTenantId.value}/notices/${id}`, id, t('notices.confirmDelete', { name }))

const formatDate = (d: string | null | undefined) => formatDateTimeShort(d, locale.value)

// Row menu: "Translations" is a link, the rest reports its key.
const menuItems = (item: GeneralNotice) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  // Only with a second content language (switched off: the page lists its default only).
  ...(trans.multilingual.value ? [{ key: 'translations', label: t('translations.manageLink'), icon: 'globe', to: `/admin/notices/${item.ID}/translations` }] : []),
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === item.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === item.ID }] : []),
]
const onMenuSelect = (item: GeneralNotice, key: string) => {
  if (key === 'edit') openEdit(item)
  else deleteItem(item.ID, item.Title) // "Translations" is a link and reports nothing
}
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" data-help-exempt="Meldung schließen, selbsterklärend" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('notices.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="canCreate" #actions>
        <OnyxButton v-help="'notices.newNotice'" :label="t('notices.newButton')" @click="openNew" />
      </template>
    </PageHeader>

    <!-- Direction C: list and form side by side (stacked below 860 px). -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('notices.cardTitle')" class="split__main">
        <template #tools>
          <SegmentControl v-model="filter" :options="filterOptions" :label="t('notices.filterLabel')" />
          <SearchBox v-model="query" v-help="'notices.search'" :label="t('notices.searchLabel')" :placeholder="t('notices.searchPlaceholder')" />
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
            <!-- Translations only via the row menu now. -->
            <RowMenu v-help="'notices.rowActions'" :label="t('patterns.menuLabel', { name: item.Title })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
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

          <FormField v-slot="f" required :label="t('notices.fieldTitle')" help="notices.title" :error="onDefaultLang ? errors.Title || null : null">
            <input :id="f.id" v-model="titleModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('notices.titlePlaceholder')" @input="clearError('Title')">
          </FormField>

          <FormField v-slot="f" required :label="t('notices.fieldMessage')" help="notices.message" :error="onDefaultLang ? errors.Message || null : null">
            <RichTextEditor :id="f.id" v-model="messageModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('notices.messagePlaceholder')" @input="clearError('Message')" />
          </FormField>

          <FormLanguageDone :trans="trans" />

          <FormField v-slot="f" required :label="t('notices.fieldLevel')" help="notices.level">
            <select :id="f.id" v-model="formData.Level" :required="f.required">
              <option v-for="opt in levelOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </FormField>

          <div class="notice-options">
            <OnyxCheckbox v-model="formData.IsActive" value="IsActive" v-help="'notices.active'" :label="t('notices.fieldActive')" />
            <OnyxCheckbox v-model="formData.Collapsed" value="Collapsed" v-help="'notices.collapsed'" :label="t('notices.fieldCollapsed')" />
            <!-- Opt-in mail sending. On creation (or first activation of an
                 inactive notice) a mail goes to all email subscribers of
                 this status page. -->
            <OnyxCheckbox v-model="formData.NotifySubscribers" value="NotifySubscribers" v-help="'notices.notifySubscribers'" :label="t('notices.fieldNotifySubscribers')" />
          </div>

          <div class="form-grid">
            <FormField v-slot="f" :label="t('notices.fieldStart')" help="notices.start">
              <input :id="f.id" v-model="formData.StartAt" type="datetime-local">
            </FormField>
            <FormField v-slot="f" :label="t('notices.fieldEnd')" help="notices.end">
              <input :id="f.id" v-model="formData.EndAt" type="datetime-local">
            </FormField>
          </div>

          </fieldset>
          <div class="form-actions">
            <OnyxButton v-help="'notices.cancel'" :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton v-if="!formReadOnly" v-help="'notices.save'" :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
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
