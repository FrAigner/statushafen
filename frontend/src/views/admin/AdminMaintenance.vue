<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { vHelp } from '../../directives/vHelp'
import { useApi, isForbidden } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useResourcePerms } from '../../composables/useResourcePerms'
import { useTenant } from '../../composables/useTenant'
import { useIncidentTemplates } from '../../composables/useIncidentTemplates'
import { useTagToggle, useAvailableTags } from '../../composables/useTagSelection'
import { useFormPanel } from '../../composables/useFormPanel'
import { useListQuery } from '../../composables/useListQuery'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useSaveAction, useDeleteAction } from '../../composables/useAdminCrudActions'
import { useFormTranslations } from '../../composables/useFormTranslations'
import FormLanguageSwitch from '../../components/FormLanguageSwitch.vue'
import FormLanguageDone from '../../components/FormLanguageDone.vue'
import { formatDateTimeShort, toLocalInputValue } from '../../utils/datetime'
import AppIcon from '../../components/AppIcon.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import SegmentControl from '../../components/SegmentControl.vue'
import FormPanel from '../../components/FormPanel.vue'
import TagField from '../../components/TagField.vue'
import RowMenu from '../../components/RowMenu.vue'
import EmptyState from '../../components/EmptyState.vue'
import { flattenComponentTree, componentOptions } from '../../utils/componentTree'
import ForbiddenNotice from '../../components/ForbiddenNotice.vue'
import ListState from '../../components/ListState.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import RichTextEditor from '../../components/RichTextEditor.vue'
import FormMultiSelect from '../../components/FormMultiSelect.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import { maintenanceBadge } from '../../utils/badgeStatus'
import { isResolvedStatus } from '../../utils/incidentStatus'
import { incidentListUrl, mergeIncidents, ADMIN_PAGE_SIZE } from '../../utils/incidentPaging'
import type { ComponentTreeNode, FlatComponent } from '../../utils/componentTree'
import type { IncidentResponse } from '../../types/api'
import type { IncidentForm } from '../../types/admin'

type ReminderUnit = 'minutes' | 'hours' | 'days'

/** Edit form: the incident form plus the reminder lead time as value and unit. */
interface MaintenanceForm extends IncidentForm {
  reminderValue?: number | string
  reminderUnit?: ReminderUnit
}

// Maintenance windows: use the same backend endpoints as incidents
// (models.Incident with IsMaintenance=true), but their own admin menu
// item/list (?is_maintenance=true), as the status here is managed
// automatically by the scheduler instead of manually.

const { apiFetch } = useApi()
const { hasPerm }  = useAuth()
// Permissions of this tab per action. Without update an existing entry opens
// read-only (formReadOnly); without delete the row menu entry is gone.
const { canCreate, canUpdate, canDelete } = useResourcePerms('maintenance')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { templates, selectedTemplateId, loadTemplates, selectedTemplate, loadTemplateTranslations } = useIncidentTemplates('maintenance')
const { t, locale } = useI18n()
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()
const breadcrumbs = usePageCrumbs(() => t('maintenance.title'))

const formData       = ref<MaintenanceForm>({})

// Language switcher above title and message (further languages of the page).
// Translations follow the tab's "update" permission.
const trans = useFormTranslations({
  apiFetch,
  urls: { list: (id) => `/api/incidents/${id}/translations/all`, save: (id, code) => `/api/incidents/${id}/translations/${code}` },
  contentField: 'message',
  formData,
  canEdit: canUpdate,
})
const { showSwitcher, onDefaultLang, titleModel, contentModel: messageModel, active: activeLang } = trans

const items          = ref<IncidentResponse[]>([])
const components     = ref<FlatComponent[]>([])
const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, activeTenantId)
const isEditing      = ref(false)
const currentEditId  = ref<number | null>(null)
const forbidden      = ref(false)
const loading        = ref(false)
const loadFailed     = ref(false)
const alertMsg       = ref('')
const alertType      = ref('warning')
const isSaving       = ref(false)
const deletingId     = ref<number | null>(null)

// Direction C: the form title names the maintenance window, not the ID.
const formTitle = computed(() => currentEditId.value
  ? t('maintenance.editTitle', { name: formData.value.Title })
  : t('maintenance.createTitle'))

const reminderUnitOptions = computed(() => [
  { label: t('maintenance.reminderMinutes'), value: 'minutes' },
  { label: t('maintenance.reminderHours'),  value: 'hours' },
  { label: t('maintenance.reminderDays'),     value: 'days' },
])
const unitToMinutes: Record<ReminderUnit, number> = { minutes: 1, hours: 60, days: 1440 }

// Stage 4b: ResponsiveTable/dataTable, status as a StatusBadge.
const columns = computed(() => [
  { key: 'Title', label: t('maintenance.tableTitle') },
  { key: 'Components', label: t('maintenance.tableComponents') },
  { key: 'Status', label: t('maintenance.tableStatus') },
  { key: 'ScheduledStartAt', label: t('maintenance.tableStart') },
  { key: 'ScheduledEndAt', label: t('maintenance.tableEnd') },
])
const componentNames = (item: IncidentResponse) => (item.Components || []).map(c => c.Name).join(', ') || '—'

/* ── List: search and state filter in the card header ── */
const isUpcoming = (item: IncidentResponse) => item.Status === 'scheduled' || item.Status === 'in_progress'
const { query, filter, rows, searching } = useListQuery(items, {
  textOf: item => `${item.Title} ${item.Message || ''} ${componentNames(item)}`,
  predicates: { upcoming: isUpcoming, resolved: item => isResolvedStatus(item.Status) },
})
const filterOptions = computed(() => [
  { value: 'all', label: t('common.all') },
  { value: 'upcoming', label: t('maintenance.filterUpcoming') },
  { value: 'resolved', label: t('maintenance.statusResolved') },
])
const upcomingCount = computed(() => items.value.filter(isUpcoming).length)
const subtitle = computed(() => t('maintenance.subtitle', {
  count: t('maintenance.countWindows', { n: items.value.length }, items.value.length),
  upcoming: upcomingCount.value,
}))

/* ── Data loading ── */
// The server returns at most 200 entries per call; "Load more" fetches the
// next ones and appends them (IDs already present stay).
const hasMore = ref(false)
const loadingMore = ref(false)
const loadMore = async () => {
  loadingMore.value = true
  try {
    const batch: IncidentResponse[] = await apiFetch(incidentListUrl(activeTenantId.value!, { maintenance: true, limit: ADMIN_PAGE_SIZE, offset: items.value.length }))
    items.value = mergeIncidents(items.value, batch)
    hasMore.value = batch.length >= ADMIN_PAGE_SIZE
  } catch (err) {
    showAlert(t('common.loadError') + (err as Error).message, 'error')
  } finally {
    loadingMore.value = false
  }
}

const loadData = async () => {
  forbidden.value = false
  loadFailed.value = false
  loading.value = true
  try {
    const [incs, comps] = await Promise.all([
      apiFetch<IncidentResponse[] | null>(incidentListUrl(activeTenantId.value!, { maintenance: true, limit: ADMIN_PAGE_SIZE })),
      apiFetch<ComponentTreeNode[]>(`/api/pages/${activeTenantId.value}/components`)
    ])
    items.value      = incs ?? []
    hasMore.value    = (incs ?? []).length >= ADMIN_PAGE_SIZE
    components.value = flattenComponentTree(comps) // including subcomponents
  } catch (err) {
    // 403 -> permission notice instead of a load error alert.
    if (isForbidden(err)) forbidden.value = true
    else {
      loadFailed.value = true
      showAlert(t('common.loadError') + (err as Error).message, 'error')
    }
  } finally {
    loading.value = false
  }
  // Tags only for global admins (see useAvailableTags).
  await loadAvailableTags()

  // Templates belong to the incident_templates:read permission and are therefore
  // loaded separately (see useIncidentTemplates), so that a 403 here does not
  // drag down loading the maintenance window list itself.
  await loadTemplates(apiFetch, activeTenantId.value, hasPerm)
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

const toggleTag = useTagToggle(formData)

const openNew = () => {
  formData.value = {
    ComponentIDs: [],
    TagIDs: [],
    ScheduledStartAt: '',
    ScheduledEndAt: '',
    reminderValue: 0,
    reminderUnit: 'hours',
  }
  selectedTemplateId.value = ''
  currentEditId.value = null
  trans.reset()
  clearErrors()
  isEditing.value = true
  focusForm()
}

// "Use template": prefills title/message/components from the chosen template;
// stays freely editable afterwards. The exact date (ScheduledStartAt/
// ScheduledEndAt) is deliberately NOT taken from the template but always has
// to be set fresh.
const applyTemplate = async () => {
  const tpl = selectedTemplate()
  if (!tpl) return
  formData.value.Title   = tpl.Title
  formData.value.Message = tpl.Message
  formData.value.ComponentIDs = (tpl.ComponentIDs || '').split(',').filter(Boolean)
  // Also take over the texts of the further languages from the template (only with the language switcher).
  if (showSwitcher.value) {
    const rows = await loadTemplateTranslations(apiFetch, activeTenantId.value, tpl.ID)
    if (selectedTemplateId.value === String(tpl.ID)) trans.setFromRows(rows)
  }
}

const onTemplateChange = (event: Event) => {
  selectedTemplateId.value = (event.target as HTMLSelectElement).value
  applyTemplate()
}

const openEdit = (item: IncidentResponse) => {
  let reminderValue = item.ReminderLeadMinutes || 0
  let reminderUnit: ReminderUnit = 'minutes'
  if (reminderValue > 0 && reminderValue % 1440 === 0) { reminderValue /= 1440; reminderUnit = 'days' }
  else if (reminderValue > 0 && reminderValue % 60 === 0) { reminderValue /= 60; reminderUnit = 'hours' }

  formData.value = {
    ...item,
    ComponentIDs: (item.Components || []).map(c => String(c.ID)),
    TagIDs: (item.Tags || []).map(t => t.ID),
    ScheduledStartAt: toLocalInputValue(item.ScheduledStartAt),
    ScheduledEndAt: toLocalInputValue(item.ScheduledEndAt),
    reminderValue,
    reminderUnit,
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
  const required = (key: string, value: unknown, labelKey: string) =>
    ({ key, value, message: t('patterns.fieldRequired', { field: t(labelKey) }) })
  const valid = await validateRequired([
    required('Title', formData.value.Title, 'maintenance.fieldTitle'),
    required('Message', formData.value.Message, 'maintenance.fieldMessage'),
    required('ScheduledStartAt', formData.value.ScheduledStartAt, 'maintenance.fieldStart'),
    required('ScheduledEndAt', formData.value.ScheduledEndAt, 'maintenance.fieldEnd'),
    required('ComponentIDs', formData.value.ComponentIDs, 'maintenance.fieldComponents'),
  ])
  if (!valid) {
    activeLang.value = '' // the required fields belong to the default language
    return
  }
  if (!trans.checkComplete(showAlert)) return
  const isNew = currentEditId.value === null
  const url   = isNew
    ? `/api/pages/${activeTenantId.value}/incidents`
    : `/api/pages/${activeTenantId.value}/incidents/${currentEditId.value}`

  const payload: Record<string, unknown> = { ...formData.value }
  payload.ComponentIDs = formData.value.ComponentIDs!.map(id => parseInt(id))
  payload.IsMaintenance = true
  payload.ReminderLeadMinutes = (Number(formData.value.reminderValue) || 0) * unitToMinutes[formData.value.reminderUnit!]
  payload.ScheduledStartAt = formData.value.ScheduledStartAt ? new Date(formData.value.ScheduledStartAt).toISOString() : null
  payload.ScheduledEndAt   = formData.value.ScheduledEndAt   ? new Date(formData.value.ScheduledEndAt).toISOString()   : null
  if (!isNew) {
    // The status is managed by the scheduler - send it unchanged (required, see UpdateIncidentHandler).
    payload.status = formData.value.Status
    payload.title = formData.value.Title
    payload.message = formData.value.Message
  } else {
    payload.Title = formData.value.Title
    payload.Message = formData.value.Message
  }

  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved: { ID?: number } | null) => {
    // Remember the new ID before translations follow: if one fails, a second attempt does not create a second entry.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value!)
    showAlert(isNew ? t('maintenance.created') : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id: number, name: string) =>
  deleteAction(`/api/pages/${activeTenantId.value}/incidents/${id}`, id, t('maintenance.confirmDelete', { name }))

const formatDate = (d: string | null | undefined) => formatDateTimeShort(d, locale.value)

// Row menu: edit only for scheduled windows (like the former edit button).
const menuItems = (item: IncidentResponse) => [
  // Edit only for scheduled windows; whoever may not update can view every window.
  ...(item.Status === 'scheduled' || !canUpdate.value ? [{ key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' }] : []),
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === item.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === item.ID }] : []),
]
const onMenuSelect = (item: IncidentResponse, key: string) => {
  if (key === 'edit') openEdit(item)
  else deleteItem(item.ID, item.Title)
}
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" data-help-exempt="Meldung schließen, selbsterklärend" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('maintenance.title')" :subtitle="forbidden ? null : subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="canCreate" #actions>
        <OnyxButton v-help="'maintenance.newWindow'" :label="t('maintenance.newButton')" @click="openNew" />
      </template>
    </PageHeader>

    <ForbiddenNotice v-if="forbidden" />

    <!-- Direction C: list and form side by side (stacked below 860 px). -->
    <div v-else class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('maintenance.cardTitle')" class="split__main">
        <template #tools>
          <SegmentControl v-model="filter" :options="filterOptions" :label="t('maintenance.filterLabel')" />
          <SearchBox v-model="query" v-help="'maintenance.search'" :label="t('maintenance.searchLabel')" :placeholder="t('maintenance.searchPlaceholder')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('patterns.searchResult', { n: rows.length }, rows.length) }}</p>

        <ResponsiveTable v-if="rows.length" data-table :columns="columns" :items="rows" row-key="ID">
          <template #cell-Title="{ item }">
            <strong class="component-name__title">{{ item.Title }}</strong>
            <small class="cell-sub">{{ item.Message }}</small>
          </template>
          <template #cell-Components="{ item }">{{ componentNames(item) }}</template>
          <template #cell-Status="{ item }">
            <StatusBadge :status="maintenanceBadge(item.Status, t).status" :label="maintenanceBadge(item.Status, t).label" />
          </template>
          <!-- Scheduled times stay absolute: "3 days ago" says nothing about the window here. -->
          <template #cell-ScheduledStartAt="{ item }"><span class="cell-changed">{{ formatDate(item.ScheduledStartAt) }}</span></template>
          <template #cell-ScheduledEndAt="{ item }"><span class="cell-changed">{{ formatDate(item.ScheduledEndAt) }}</span></template>
          <template #actions="{ item }">
            <RowMenu v-help="'maintenance.rowActions'" :label="t('patterns.menuLabel', { name: item.Title })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="items.length" :title="t('maintenance.noResults')" :text="t('maintenance.noResultsText')" />
        <ListState v-else :loading="loading" :error="loadFailed" :count="items.length" :empty-text="t('maintenance.empty')" icon="clock" />
        <div v-if="hasMore" class="list-more">
          <OnyxButton v-help="'maintenance.loadMore'" :label="t('incidents.loadMore')" mode="outline" :disabled="loadingMore" data-test="load-more" @click="loadMore" />
        </div>
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
          <FormLanguageSwitch :trans="trans" />
          <fieldset class="form-lock" :disabled="formReadOnly">

          <FormField v-if="currentEditId === null && templates.length > 0" v-slot="f" :label="t('maintenance.templateLabel')" help="maintenance.template">
            <select :id="f.id" :value="selectedTemplateId" @change="onTemplateChange">
              <option value="">{{ t('maintenance.noTemplatePlaceholder') }}</option>
              <option v-for="tpl in templates" :key="tpl.ID" :value="String(tpl.ID)">{{ tpl.Name }}</option>
            </select>
          </FormField>

          <FormField v-slot="f" required :label="t('maintenance.fieldTitle')" help="maintenance.title" :error="onDefaultLang ? errors.Title || null : null">
            <input :id="f.id" v-model="titleModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('maintenance.titlePlaceholder')" @input="clearError('Title')">
          </FormField>

          <FormField v-slot="f" required :label="t('maintenance.fieldMessage')" help="maintenance.message" :error="onDefaultLang ? errors.Message || null : null">
            <RichTextEditor :id="f.id" v-model="messageModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('maintenance.messagePlaceholder')" @input="clearError('Message')" />
          </FormField>

          <FormLanguageDone :trans="trans" />

          <!-- Multi-select in the FormField pattern (label association,
               required-field error at the field). -->
          <FormMultiSelect
            v-model="formData.ComponentIDs"
            :label="t('maintenance.fieldComponents')"
            help="maintenance.components"
            :options="componentOptions(components, formData.ComponentIDs)"
            required
            :error="errors.ComponentIDs || null"
            @update:model-value="clearError('ComponentIDs')"
          />

          <TagField v-if="canSelectTags" v-help="'maintenance.tags'" :label="t('maintenance.fieldTags')" :tags="availableTags" :selected-ids="formData.TagIDs!" @toggle="toggleTag" />

          <div class="form-grid">
            <FormField v-slot="f" required :label="t('maintenance.fieldStart')" help="maintenance.start" :error="errors.ScheduledStartAt || null">
              <input :id="f.id" v-model="formData.ScheduledStartAt" type="datetime-local" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @input="clearError('ScheduledStartAt')">
            </FormField>
            <FormField v-slot="f" required :label="t('maintenance.fieldEnd')" help="maintenance.end" :error="errors.ScheduledEndAt || null">
              <input :id="f.id" v-model="formData.ScheduledEndAt" type="datetime-local" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @input="clearError('ScheduledEndAt')">
            </FormField>
          </div>

          <div class="form-grid">
            <FormField v-slot="f" :label="t('maintenance.fieldReminderValue')" help="maintenance.reminder">
              <input :id="f.id" v-model="formData.reminderValue" type="number" min="0">
            </FormField>
            <FormField v-slot="f" :label="t('maintenance.fieldReminderUnit')" help="maintenance.reminder">
              <select :id="f.id" v-model="formData.reminderUnit">
                <option v-for="opt in reminderUnitOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
              </select>
            </FormField>
          </div>

          </fieldset>
          <div class="form-actions">
            <OnyxButton v-help="'maintenance.cancel'" :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton v-if="!formReadOnly" v-help="'maintenance.save'" :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>

  </div>
</template>

<style scoped>
.list-more {
  display: flex;
  justify-content: center;
  padding: var(--sp-4) 0 var(--sp-2);
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
