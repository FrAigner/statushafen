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
import { toLocalInputValue } from '../../utils/datetime'
import AppIcon from '../../components/AppIcon.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import SegmentControl from '../../components/SegmentControl.vue'
import FormPanel from '../../components/FormPanel.vue'
import TagField from '../../components/TagField.vue'
import RelativeTime from '../../components/RelativeTime.vue'
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
import { incidentBadge } from '../../utils/badgeStatus'
import { isResolvedStatus } from '../../utils/incidentStatus'
import { incidentListUrl, mergeIncidents, ADMIN_PAGE_SIZE } from '../../utils/incidentPaging'

const { apiFetch } = useApi()
const { hasPerm }  = useAuth()
// Rechte dieses Tabs je Aktion. Ohne Ändern öffnet sich ein bestehender
// Eintrag schreibgeschützt (formReadOnly), ohne Löschen entfällt der Eintrag im Zeilenmenü.
const { canCreate, canUpdate, canDelete } = useResourcePerms('incidents')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { templates, selectedTemplateId, loadTemplates, selectedTemplate, loadTemplateTranslations } = useIncidentTemplates('incident')
const { t } = useI18n()
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()
const breadcrumbs = usePageCrumbs(() => t('incidents.title'))

const formData       = ref({})

// Sprachumschalter über Titel und Nachricht (weitere Sprachen der Seite).
// Übersetzungen folgen dem Recht „Ändern“ des Tabs (wie die Übersetzungsseite).
const trans = useFormTranslations({
  apiFetch,
  urls: {
    list: (id) => `/api/incidents/${id}/translations/all`,
    save: (id, code) => `/api/incidents/${id}/translations/${code}`,
  },
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

// Richtung C: Titel des Formulars nennt den Vorfall, nicht die ID.
const formTitle = computed(() => currentEditId.value
  ? t('incidents.editTitle', { name: formData.value.Title })
  : t('incidents.createTitle'))

const statusOptions = computed(() => ['investigating', 'identified', 'monitoring', 'resolved']
  .map(value => ({ label: incidentBadge(value, t).label, value })))

// Schweregrad (Stufen des Komponentenstatus) gewichtet die Verfügbarkeit.
const severityOptions = computed(() => [
  { value: 'degraded_performance', label: t('status.degradedPerformance') },
  { value: 'partial_outage', label: t('status.partialOutage') },
  { value: 'major_outage', label: t('status.majorOutage') },
])

// Stufe 4b: ResponsiveTable/dataTable statt eigener Tabelle. Der
// Status steht als StatusBadge (Form + Farbe + Text); die frühere Randfarbe je
// Zeile entfällt, weil sie nur über Farbe lief.
const columns = computed(() => [
  { key: 'Title', label: t('incidents.tableTitle') },
  { key: 'Components', label: t('incidents.tableComponents') },
  { key: 'Status', label: t('incidents.tableStatus') },
  { key: 'CreatedAt', label: t('incidents.tableCreated') },
])
const componentNames = (item) => (item.Components || []).map(c => c.Name).join(', ') || '—'

/* ── Liste: Suche und Statusfilter im Kartenkopf ── */
// dieselbe Regel wie Statusseite und Backend (nur resolved, Altwert completed seit der Umstellung migriert).
const isOpen = (item) => !isResolvedStatus(item.Status)
const { query, filter, rows, searching, filtered } = useListQuery(items, {
  textOf: item => `${item.Title} ${item.Message || ''} ${componentNames(item)}`,
  predicates: { open: isOpen, resolved: item => !isOpen(item) },
})
const filterOptions = computed(() => [
  { value: 'all', label: t('common.all') },
  { value: 'open', label: t('incidents.filterOpen') },
  { value: 'resolved', label: t('incidents.filterResolved') },
])
const openCount = computed(() => items.value.filter(isOpen).length)
const subtitle = computed(() => t('incidents.subtitle', {
  count: t('incidents.countIncidents', { n: items.value.length }, items.value.length),
  open: openCount.value,
}))

/* ── Data loading ── */
// Der Server liefert höchstens 200 Einträge je Aufruf; „Weitere laden“ holt die
// nächsten und hängt sie an (schon vorhandene IDs bleiben).
const hasMore = ref(false)
const loadingMore = ref(false)
const loadMore = async () => {
  loadingMore.value = true
  try {
    const batch = await apiFetch(incidentListUrl(activeTenantId.value, {  limit: ADMIN_PAGE_SIZE, offset: items.value.length }))
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
      apiFetch(incidentListUrl(activeTenantId.value, { limit: ADMIN_PAGE_SIZE })),
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
  // das eigentliche Laden der Vorfallsliste mitreißt (Promise.all würde
  // sonst insgesamt fehlschlagen).
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
  formData.value  = { Status: 'investigating', Severity: 'degraded_performance', ComponentIDs: [], TagIDs: [], StartedAt: '', ResolvedAt: '' }
  selectedTemplateId.value = ''
  currentEditId.value = null
  trans.reset()
  clearErrors()
  isEditing.value = true
  focusForm()
}

// "Vorlage verwenden": befüllt Titel/Message/Status/Komponenten
// aus der gewählten Vorlage vor, bleibt danach aber frei editierbar - es
// wird nur einmalig kopiert, keine laufende Bindung an die Vorlage.
const applyTemplate = async () => {
  const tpl = selectedTemplate()
  if (!tpl) return
  formData.value.Title   = tpl.Title
  formData.value.Message = tpl.Message
  if (tpl.DefaultStatus) formData.value.Status = tpl.DefaultStatus
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
  formData.value  = {
    ...item,
    ComponentIDs: (item.Components || []).map(c => String(c.ID)),
    TagIDs: (item.Tags || []).map(t => t.ID),
    StartedAt: toLocalInputValue(item.StartedAt),
    ResolvedAt: toLocalInputValue(item.ResolvedAt),
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
    { key: 'Title', value: formData.value.Title, message: t('patterns.fieldRequired', { field: t('incidents.fieldTitle') }) },
    { key: 'Message', value: formData.value.Message, message: t('patterns.fieldRequired', { field: t('incidents.fieldMessage') }) },
    { key: 'ComponentIDs', value: formData.value.ComponentIDs, message: t('patterns.fieldRequired', { field: t('incidents.fieldComponents') }) },
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
  // Ereigniszeiten: lokale Eingabe -> ISO-String, leer -> null.
  payload.StartedAt  = formData.value.StartedAt  ? new Date(formData.value.StartedAt).toISOString()  : null
  payload.ResolvedAt = formData.value.ResolvedAt ? new Date(formData.value.ResolvedAt).toISOString() : null
  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved) => {
    // Neue ID merken, bevor Übersetzungen folgen: scheitert eine, legt ein zweiter Versuch keinen zweiten Vorfall an.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value)
    showAlert(isNew ? t('incidents.created') : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id, name) =>
  deleteAction(`/api/pages/${activeTenantId.value}/incidents/${id}`, id, t('incidents.confirmDelete', { name }))

// Zeilenmenü: „Übersetzungen“ ist ein Link, der Rest meldet seinen key.
const menuItems = (item) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  // Schnellzugriff: springt zum Update-Formular des Vorfalls
  ...(canCreate.value ? [{ key: 'update', label: t('incidents.postUpdate'), icon: 'message-circle', to: `/admin/incidents/${activeTenantId.value}/${item.ID}#update` }] : []),
  { key: 'translations', label: t('translations.manageLink'), icon: 'globe', to: `/admin/incidents/${item.ID}/translations` },
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === item.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === item.ID }] : []),
]
const onMenuSelect = (item, key) => {
  if (key === 'edit') openEdit(item)
  else deleteItem(item.ID, item.Title) // „Übersetzungen“ und „Update posten“ sind Links und melden nichts
}
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('incidents.title')" :subtitle="forbidden ? null : subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="canCreate" #actions>
        <OnyxButton :label="t('incidents.newButton')" @click="openNew" />
      </template>
    </PageHeader>

    <ForbiddenNotice v-if="forbidden" />

    <!-- Richtung C: Liste und Formular nebeneinander (unter 860 px untereinander). -->
    <div v-else class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('incidents.cardTitle')" class="split__main">
        <template #tools>
          <SegmentControl v-model="filter" :options="filterOptions" :label="t('incidents.filterLabel')" />
          <SearchBox v-model="query" :label="t('incidents.searchLabel')" :placeholder="t('incidents.searchPlaceholder')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('patterns.searchResult', { n: rows.length }, rows.length) }}</p>

        <ResponsiveTable v-if="rows.length" data-table :columns="columns" :items="rows" row-key="ID">
          <template #cell-Title="{ item }">
            <div class="component-name">
              <router-link :to="`/admin/incidents/${activeTenantId}/${item.ID}`"><strong class="component-name__title">{{ item.Title }}</strong></router-link>
            </div>
            <small class="cell-sub">{{ item.Message }}</small>
          </template>
          <template #cell-Components="{ item }">{{ componentNames(item) }}</template>
          <template #cell-Status="{ item }">
            <StatusBadge :status="incidentBadge(item.Status, t).status" :label="incidentBadge(item.Status, t).label" />
          </template>
          <template #cell-CreatedAt="{ item }"><RelativeTime :value="item.CreatedAt" /></template>
          <template #actions="{ item }">
            <!-- Übersetzungen nur noch über das Zeilenmenü. -->
            <RowMenu :label="t('patterns.menuLabel', { name: item.Title })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="items.length" :title="t('incidents.noResults')" :text="t('incidents.noResultsText')" />
        <ListState v-else :loading="loading" :error="loadFailed" :count="items.length" :empty-text="t('incidents.empty')" icon="alert-triangle" />
        <div v-if="hasMore" class="list-more">
          <OnyxButton :label="t('incidents.loadMore')" mode="outline" :disabled="loadingMore" data-test="load-more" @click="loadMore" />
        </div>
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
          <FormLanguageSwitch :trans="trans" />
          <fieldset class="form-lock" :disabled="formReadOnly">

          <FormField v-if="currentEditId === null && templates.length > 0" v-slot="f" :label="t('incidents.templateLabel')">
            <select :id="f.id" :value="selectedTemplateId" @change="onTemplateChange">
              <option value="">{{ t('incidents.noTemplatePlaceholder') }}</option>
              <option v-for="tpl in templates" :key="tpl.ID" :value="String(tpl.ID)">{{ tpl.Name }}</option>
            </select>
          </FormField>

          <FormField v-slot="f" required :label="t('incidents.fieldTitle')" :error="onDefaultLang ? errors.Title || null : null">
            <input :id="f.id" v-model="titleModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('incidents.titlePlaceholder')" @input="clearError('Title')">
          </FormField>

          <FormField v-slot="f" required :label="t('incidents.fieldMessage')" :error="onDefaultLang ? errors.Message || null : null">
            <RichTextEditor :id="f.id" v-model="messageModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('incidents.messagePlaceholder')" @input="clearError('Message')" />
          </FormField>

          <FormLanguageDone :trans="trans" />

          <FormField v-if="currentEditId === null" v-slot="f" required :label="t('incidents.fieldStatus')">
            <select :id="f.id" v-model="formData.Status" :required="f.required">
              <option v-for="opt in statusOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </FormField>
          <div v-else class="edit-status-hint">
            {{ t('incidents.statusHintPrefix') }}
            <router-link :to="`/admin/incidents/${activeTenantId}/${currentEditId}`">{{ t('incidents.statusHintLink') }}</router-link>
          </div>

          <FormField v-slot="f" :label="t('incidents.fieldSeverity')" :hint="t('incidents.fieldSeverityHint')">
            <select :id="f.id" v-model="formData.Severity" :aria-describedby="f.describedby" data-test="severity">
              <option v-for="opt in severityOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </FormField>

          <div class="form-grid">
            <FormField v-slot="f" :label="t('incidents.fieldStartedAt')">
              <input :id="f.id" v-model="formData.StartedAt" type="datetime-local" data-test="started-at">
            </FormField>
            <FormField v-slot="f" :label="t('incidents.fieldResolvedAt')">
              <input :id="f.id" v-model="formData.ResolvedAt" type="datetime-local" data-test="resolved-at">
            </FormField>
          </div>

          <!-- Mehrfachauswahl im FormField-Muster (Label-Verknüpfung,
               Pflichtfeld-Fehler am Feld). -->
          <FormMultiSelect
            v-model="formData.ComponentIDs"
            :label="t('incidents.fieldComponents')"
            :options="componentOptions(components, formData.ComponentIDs)"
            required
            :error="errors.ComponentIDs || null"
            @update:model-value="clearError('ComponentIDs')"
          />

          <TagField v-if="canSelectTags" :label="t('incidents.fieldTags')" :tags="availableTags" :selected-ids="formData.TagIDs" @toggle="toggleTag" />

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
.edit-status-hint {
  font-size: var(--fs-sm);
  color: var(--text-2);
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
