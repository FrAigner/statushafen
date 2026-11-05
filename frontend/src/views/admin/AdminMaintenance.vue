<script setup>
import { ref, computed, watch } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
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

// Wartungsfenster: nutzt dieselben Backend-Endpunkte wie
// Incidents (models.Incident mit IsMaintenance=true), aber eigener
// Admin-Menüpunkt/eigene Liste (?is_maintenance=true), da Status hier
// automatisch vom Scheduler verwaltet wird statt manuell.

const { apiFetch } = useApi()
const { hasPerm }  = useAuth()
// Rechte dieses Tabs je Aktion. Ohne Ändern öffnet sich ein bestehender
// Eintrag schreibgeschützt (formReadOnly), ohne Löschen entfällt der Eintrag im Zeilenmenü.
const { canCreate, canUpdate, canDelete } = useResourcePerms('maintenance')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { templates, selectedTemplateId, loadTemplates, selectedTemplate, loadTemplateTranslations } = useIncidentTemplates('maintenance')
const { t, locale } = useI18n()
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()
const breadcrumbs = usePageCrumbs(() => t('maintenance.title'))

const formData       = ref({})

// Sprachumschalter über Titel und Nachricht (weitere Sprachen der Seite).
// Übersetzungen folgen dem Recht „Ändern“ des Tabs.
const trans = useFormTranslations({
  apiFetch,
  urls: { list: (id) => `/api/incidents/${id}/translations/all`, save: (id, code) => `/api/incidents/${id}/translations/${code}` },
  contentField: 'message',
  formData,
  canEdit: canUpdate,
})
const { showSwitcher, onDefaultLang, titleModel, contentModel: messageModel, active: activeLang } = trans

const items          = ref([])
const components     = ref([])
const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, activeTenantId)
const isEditing      = ref(false)
const currentEditId  = ref(null)
const forbidden      = ref(false)
const loading        = ref(false)
const loadFailed     = ref(false)
const alertMsg       = ref('')
const alertType      = ref('warning')
const isSaving       = ref(false)
const deletingId     = ref(null)

// Richtung C: Titel des Formulars nennt das Wartungsfenster, nicht die ID.
const formTitle = computed(() => currentEditId.value
  ? t('maintenance.editTitle', { name: formData.value.Title })
  : t('maintenance.createTitle'))

const reminderUnitOptions = computed(() => [
  { label: t('maintenance.reminderMinutes'), value: 'minutes' },
  { label: t('maintenance.reminderHours'),  value: 'hours' },
  { label: t('maintenance.reminderDays'),     value: 'days' },
])
const unitToMinutes = { minutes: 1, hours: 60, days: 1440 }

// Stufe 4b: ResponsiveTable/dataTable, Status als StatusBadge.
const columns = computed(() => [
  { key: 'Title', label: t('maintenance.tableTitle') },
  { key: 'Components', label: t('maintenance.tableComponents') },
  { key: 'Status', label: t('maintenance.tableStatus') },
  { key: 'ScheduledStartAt', label: t('maintenance.tableStart') },
  { key: 'ScheduledEndAt', label: t('maintenance.tableEnd') },
])
const componentNames = (item) => (item.Components || []).map(c => c.Name).join(', ') || '—'

/* ── Liste: Suche und Zustandsfilter im Kartenkopf ── */
const isUpcoming = (item) => item.Status === 'scheduled' || item.Status === 'in_progress'
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
// Der Server liefert höchstens 200 Einträge je Aufruf; „Weitere laden“ holt die
// nächsten und hängt sie an (schon vorhandene IDs bleiben).
const hasMore = ref(false)
const loadingMore = ref(false)
const loadMore = async () => {
  loadingMore.value = true
  try {
    const batch = await apiFetch(incidentListUrl(activeTenantId.value, { maintenance: true, limit: ADMIN_PAGE_SIZE, offset: items.value.length }))
    items.value = mergeIncidents(items.value, batch)
    hasMore.value = batch.length >= ADMIN_PAGE_SIZE
  } catch (err) {
    showAlert(t('common.loadError') + err.message, 'error')
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
      apiFetch(incidentListUrl(activeTenantId.value, { maintenance: true, limit: ADMIN_PAGE_SIZE })),
      apiFetch(`/api/pages/${activeTenantId.value}/components`)
    ])
    items.value      = incs ?? []
    hasMore.value    = (incs ?? []).length >= ADMIN_PAGE_SIZE
    components.value = flattenComponentTree(comps) // inkl. Unterkomponenten
  } catch (err) {
    // 403 -> Berechtigungsmeldung statt Ladefehler-Alert.
    if (isForbidden(err)) forbidden.value = true
    else {
      loadFailed.value = true
      showAlert(t('common.loadError') + err.message, 'error')
    }
  } finally {
    loading.value = false
  }
  // Tags nur für Global Admins (siehe useAvailableTags).
  await loadAvailableTags()

  // Vorlagen gehören zur incident_templates:read-Berechtigung und werden deshalb
  // separat geladen (siehe useIncidentTemplates), damit ein 403 hier nicht
  // das eigentliche Laden der Wartungsfenster-Liste mitreißt.
  await loadTemplates(apiFetch, activeTenantId.value, hasPerm)
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

// "Vorlage verwenden": befüllt Titel/Message/Komponenten aus der
// gewählten Vorlage vor, bleibt danach frei editierbar. Das exakte Datum
// (ScheduledStartAt/ScheduledEndAt) wird bewusst NICHT aus der Vorlage
// übernommen, sondern bleibt immer frisch zu setzen.
const applyTemplate = async () => {
  const tpl = selectedTemplate()
  if (!tpl) return
  formData.value.Title   = tpl.Title
  formData.value.Message = tpl.Message
  formData.value.ComponentIDs = (tpl.ComponentIDs || '').split(',').filter(Boolean)
  // Texte der weiteren Sprachen aus der Vorlage mit übernehmen (nur bei Sprachumschalter).
  if (showSwitcher.value) {
    const rows = await loadTemplateTranslations(apiFetch, activeTenantId.value, tpl.ID)
    if (selectedTemplateId.value === String(tpl.ID)) trans.setFromRows(rows)
  }
}

const onTemplateChange = (event) => {
  selectedTemplateId.value = event.target.value
  applyTemplate()
}

const openEdit = (item) => {
  let reminderValue = item.ReminderLeadMinutes || 0
  let reminderUnit = 'minutes'
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
  // Pflichtfelder: Fehler am Feld statt eines Serverfehlers, Fokus aufs erste.
  const required = (key, value, labelKey) =>
    ({ key, value, message: t('patterns.fieldRequired', { field: t(labelKey) }) })
  const valid = await validateRequired([
    required('Title', formData.value.Title, 'maintenance.fieldTitle'),
    required('Message', formData.value.Message, 'maintenance.fieldMessage'),
    required('ScheduledStartAt', formData.value.ScheduledStartAt, 'maintenance.fieldStart'),
    required('ScheduledEndAt', formData.value.ScheduledEndAt, 'maintenance.fieldEnd'),
    required('ComponentIDs', formData.value.ComponentIDs, 'maintenance.fieldComponents'),
  ])
  if (!valid) {
    activeLang.value = '' // die Pflichtfelder gehören zur Standardsprache
    return
  }
  if (!trans.checkComplete(showAlert)) return
  const isNew = currentEditId.value === null
  const url   = isNew
    ? `/api/pages/${activeTenantId.value}/incidents`
    : `/api/pages/${activeTenantId.value}/incidents/${currentEditId.value}`

  const payload = { ...formData.value }
  payload.ComponentIDs = formData.value.ComponentIDs.map(id => parseInt(id))
  payload.IsMaintenance = true
  payload.ReminderLeadMinutes = (Number(formData.value.reminderValue) || 0) * unitToMinutes[formData.value.reminderUnit]
  payload.ScheduledStartAt = formData.value.ScheduledStartAt ? new Date(formData.value.ScheduledStartAt).toISOString() : null
  payload.ScheduledEndAt   = formData.value.ScheduledEndAt   ? new Date(formData.value.ScheduledEndAt).toISOString()   : null
  if (!isNew) {
    // Status wird vom Scheduler verwaltet - unverändert mitsenden (required, s. UpdateIncidentHandler).
    payload.status = formData.value.Status
    payload.title = formData.value.Title
    payload.message = formData.value.Message
  } else {
    payload.Title = formData.value.Title
    payload.Message = formData.value.Message
  }

  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved) => {
    // Neue ID merken, bevor Übersetzungen folgen: scheitert eine, legt ein zweiter Versuch keinen zweiten Eintrag an.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value)
    showAlert(isNew ? t('maintenance.created') : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id, name) =>
  deleteAction(`/api/pages/${activeTenantId.value}/incidents/${id}`, id, t('maintenance.confirmDelete', { name }))

const formatDate = (d) => formatDateTimeShort(d, locale.value)

// Zeilenmenü: Bearbeiten nur für geplante Fenster (wie zuvor der Bearbeiten-Knopf).
const menuItems = (item) => [
  // Bearbeiten nur für geplante Fenster; wer nicht ändern darf, kann jedes Fenster ansehen.
  ...(item.Status === 'scheduled' || !canUpdate.value ? [{ key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' }] : []),
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === item.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === item.ID }] : []),
]
const onMenuSelect = (item, key) => {
  if (key === 'edit') openEdit(item)
  else deleteItem(item.ID, item.Title)
}
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('maintenance.title')" :subtitle="forbidden ? null : subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="canCreate" #actions>
        <OnyxButton :label="t('maintenance.newButton')" @click="openNew" />
      </template>
    </PageHeader>

    <ForbiddenNotice v-if="forbidden" />

    <!-- Richtung C: Liste und Formular nebeneinander (unter 860 px untereinander). -->
    <div v-else class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('maintenance.cardTitle')" class="split__main">
        <template #tools>
          <SegmentControl v-model="filter" :options="filterOptions" :label="t('maintenance.filterLabel')" />
          <SearchBox v-model="query" :label="t('maintenance.searchLabel')" :placeholder="t('maintenance.searchPlaceholder')" />
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
          <!-- Geplante Zeitpunkte bleiben absolut: „vor 3 Tagen“ sagt hier nichts über das Fenster. -->
          <template #cell-ScheduledStartAt="{ item }"><span class="cell-changed">{{ formatDate(item.ScheduledStartAt) }}</span></template>
          <template #cell-ScheduledEndAt="{ item }"><span class="cell-changed">{{ formatDate(item.ScheduledEndAt) }}</span></template>
          <template #actions="{ item }">
            <RowMenu :label="t('patterns.menuLabel', { name: item.Title })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="items.length" :title="t('maintenance.noResults')" :text="t('maintenance.noResultsText')" />
        <ListState v-else :loading="loading" :error="loadFailed" :count="items.length" :empty-text="t('maintenance.empty')" icon="clock" />
        <div v-if="hasMore" class="list-more">
          <OnyxButton :label="t('incidents.loadMore')" mode="outline" :disabled="loadingMore" data-test="load-more" @click="loadMore" />
        </div>
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
          <FormLanguageSwitch :trans="trans" />
          <fieldset class="form-lock" :disabled="formReadOnly">

          <FormField v-if="currentEditId === null && templates.length > 0" v-slot="f" :label="t('maintenance.templateLabel')">
            <select :id="f.id" :value="selectedTemplateId" @change="onTemplateChange">
              <option value="">{{ t('maintenance.noTemplatePlaceholder') }}</option>
              <option v-for="tpl in templates" :key="tpl.ID" :value="String(tpl.ID)">{{ tpl.Name }}</option>
            </select>
          </FormField>

          <FormField v-slot="f" required :label="t('maintenance.fieldTitle')" :error="onDefaultLang ? errors.Title || null : null">
            <input :id="f.id" v-model="titleModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('maintenance.titlePlaceholder')" @input="clearError('Title')">
          </FormField>

          <FormField v-slot="f" required :label="t('maintenance.fieldMessage')" :error="onDefaultLang ? errors.Message || null : null">
            <RichTextEditor :id="f.id" v-model="messageModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('maintenance.messagePlaceholder')" @input="clearError('Message')" />
          </FormField>

          <FormLanguageDone :trans="trans" />

          <!-- Mehrfachauswahl im FormField-Muster (Label-Verknüpfung,
               Pflichtfeld-Fehler am Feld). -->
          <FormMultiSelect
            v-model="formData.ComponentIDs"
            :label="t('maintenance.fieldComponents')"
            :options="componentOptions(components, formData.ComponentIDs)"
            required
            :error="errors.ComponentIDs || null"
            @update:model-value="clearError('ComponentIDs')"
          />

          <TagField v-if="canSelectTags" :label="t('maintenance.fieldTags')" :tags="availableTags" :selected-ids="formData.TagIDs" @toggle="toggleTag" />

          <div class="form-grid">
            <FormField v-slot="f" required :label="t('maintenance.fieldStart')" :error="errors.ScheduledStartAt || null">
              <input :id="f.id" v-model="formData.ScheduledStartAt" type="datetime-local" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @input="clearError('ScheduledStartAt')">
            </FormField>
            <FormField v-slot="f" required :label="t('maintenance.fieldEnd')" :error="errors.ScheduledEndAt || null">
              <input :id="f.id" v-model="formData.ScheduledEndAt" type="datetime-local" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @input="clearError('ScheduledEndAt')">
            </FormField>
          </div>

          <div class="form-grid">
            <FormField v-slot="f" :label="t('maintenance.fieldReminderValue')">
              <input :id="f.id" v-model="formData.reminderValue" type="number" min="0">
            </FormField>
            <FormField v-slot="f" :label="t('maintenance.fieldReminderUnit')">
              <select :id="f.id" v-model="formData.reminderUnit">
                <option v-for="opt in reminderUnitOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
              </select>
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
