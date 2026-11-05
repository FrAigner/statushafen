<script setup>
import { ref, computed, watch } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useResourcePerms } from '../../composables/useResourcePerms'
import { useTenant } from '../../composables/useTenant'
import { useSaveAction, useDeleteAction } from '../../composables/useAdminCrudActions'
import AppIcon from '../../components/AppIcon.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import RichTextEditor from '../../components/RichTextEditor.vue'
import FormMultiSelect from '../../components/FormMultiSelect.vue'
import EmptyState from '../../components/EmptyState.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import SegmentControl from '../../components/SegmentControl.vue'
import FormPanel from '../../components/FormPanel.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import RowMenu from '../../components/RowMenu.vue'
import { incidentBadge } from '../../utils/badgeStatus'
import { useFormPanel } from '../../composables/useFormPanel'
import { useFormTranslations } from '../../composables/useFormTranslations'
import FormLanguageSwitch from '../../components/FormLanguageSwitch.vue'
import FormLanguageDone from '../../components/FormLanguageDone.vue'
import { useListQuery } from '../../composables/useListQuery'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { flattenComponentTree, componentOptions } from '../../utils/componentTree'

// Vorlagen für Vorfälle/Wartungsfenster: CRUD analog zu
// AdminComponents.vue. Werden im "Vorlage verwenden"-Dropdown von
// AdminIncidents.vue/AdminMaintenance.vue zum Vorbefüllen genutzt - bewusst
// ohne FK-Beziehung zurück, damit das Löschen einer Vorlage bereits
// erstellte Incidents/Wartungsfenster nicht beeinflusst.

const { apiFetch } = useApi()
// Rechte dieses Tabs je Aktion. Ohne Ändern öffnet sich ein bestehender
// Eintrag schreibgeschützt (formReadOnly), ohne Löschen entfällt der Eintrag im Zeilenmenü.
const { canCreate, canUpdate, canDelete } = useResourcePerms('incident_templates')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { t } = useI18n()
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()
const breadcrumbs = usePageCrumbs(() => t('incidentTemplates.title'))

const items          = ref([])
const components     = ref([])
const isEditing      = ref(false)
const currentEditId  = ref(null)
const formData       = ref({})
const alertMsg       = ref('')
const alertType      = ref('warning')
const isSaving       = ref(false)
const deletingId     = ref(null)

// Sprachumschalter über Titel und Nachricht (weitere Sprachen der Seite).
// Übersetzungen folgen dem Recht „Ändern“ des Tabs.
const templateUrl = (id) => `/api/pages/${activeTenantId.value}/incident-templates/${id}/translations`
const trans = useFormTranslations({
  apiFetch,
  urls: { list: templateUrl, save: (id, code) => `${templateUrl(id)}/${code}` },
  contentField: 'message',
  formData,
  canEdit: canUpdate,
})
const { showSwitcher, onDefaultLang, titleModel, contentModel: messageModel, active: activeLang } = trans

const kindOptions = computed(() => [
  { label: t('incidentTemplates.kindIncident'),    value: 'incident' },
  { label: t('incidentTemplates.kindMaintenance'), value: 'maintenance' },
])

// Beschriftungen wie in den Vorfall-Ansichten (vorher fest englisch im Quelltext).
const statusOptions = computed(() => ['investigating', 'identified', 'monitoring', 'resolved']
  .map(value => ({ label: incidentBadge(value, t).label, value })))

// Richtung C: Titel des Formulars nennt die Vorlage, nicht die ID.
const formTitle = computed(() => currentEditId.value
  ? t('incidentTemplates.editTitle', { name: formData.value.Name })
  : t('incidentTemplates.createTitle'))

const kindLabel = (kind) => kindOptions.value.find(o => o.value === kind)?.label ?? kind

const columns = computed(() => [
  { key: 'Name', label: t('incidentTemplates.tableName') },
  { key: 'Kind', label: t('incidentTemplates.tableKind') },
  { key: 'Title', label: t('incidentTemplates.tableTitle') },
  { key: 'ComponentIDs', label: t('incidentTemplates.tableComponents') },
])

/* ── Liste: Suche und Typfilter im Kartenkopf ── */
const { query, filter, rows, searching } = useListQuery(items, {
  textOf: tpl => `${tpl.Name} ${tpl.Title || ''} ${tpl.Message || ''}`,
  predicates: { incident: tpl => tpl.Kind === 'incident', maintenance: tpl => tpl.Kind === 'maintenance' },
})
const filterOptions = computed(() => [
  { value: 'all', label: t('common.all') },
  ...kindOptions.value,
])
const subtitle = computed(() => t('incidentTemplates.subtitle', {
  count: t('incidentTemplates.countTemplates', { n: items.value.length }, items.value.length),
}))

/* ── Data loading ── */
const loadData = async () => {
  try {
    const [tpls, comps] = await Promise.all([
      apiFetch(`/api/pages/${activeTenantId.value}/incident-templates`),
      apiFetch(`/api/pages/${activeTenantId.value}/components`)
    ])
    items.value      = tpls
    components.value = flattenComponentTree(comps) // inkl. Unterkomponenten
  } catch (err) {
    showAlert(t('common.loadError') + err.message, 'error')
  }
}

watch(activeTenantId, (newId) => {
  if (newId) {
    loadData()
    trans.load(newId)
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
  formData.value  = { Kind: 'incident', DefaultStatus: 'investigating', ComponentIDs: [] }
  currentEditId.value = null
  trans.reset()
  clearErrors()
  isEditing.value = true
  focusForm()
}

const openEdit = (item) => {
  formData.value  = {
    ...item,
    ComponentIDs: (item.ComponentIDs || '').split(',').filter(Boolean),
  }
  currentEditId.value = item.ID
  if (canUpdate.value) trans.loadFor(item.ID)
  else trans.reset()
  clearErrors()
  isEditing.value = true
  focusForm()
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
  const required = (key, labelKey) =>
    ({ key, value: formData.value[key], message: t('patterns.fieldRequired', { field: t(labelKey) }) })
  const valid = await validateRequired([
    required('Name', 'incidentTemplates.fieldName'),
    required('Title', 'incidentTemplates.fieldTitle'),
    required('Message', 'incidentTemplates.fieldMessage'),
  ])
  if (!valid) {
    activeLang.value = '' // die Pflichtfelder gehören zur Standardsprache
    return
  }
  if (!trans.checkComplete(showAlert)) return
  const isNew = currentEditId.value === null
  const url   = isNew
    ? `/api/pages/${activeTenantId.value}/incident-templates`
    : `/api/pages/${activeTenantId.value}/incident-templates/${currentEditId.value}`
  const payload = { ...formData.value }
  payload.ComponentIDs = (formData.value.ComponentIDs || []).join(',')
  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved) => {
    // Neue ID merken, bevor Übersetzungen folgen: scheitert eine, legt ein zweiter Versuch keine zweite Vorlage an.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value)
    showAlert(isNew ? t('incidentTemplates.created') : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id, name) =>
  deleteAction(`/api/pages/${activeTenantId.value}/incident-templates/${id}`, id, t('incidentTemplates.confirmDelete', { name }))

// Zeilenmenü: Bearbeiten, Löschen.
const menuItems = (tpl) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === tpl.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === tpl.ID }] : []),
]
const onMenuSelect = (tpl, key) => {
  if (key === 'edit') openEdit(tpl)
  else deleteItem(tpl.ID, tpl.Name)
}

const componentNames = (ids) => {
  const idList = (ids || '').split(',').filter(Boolean)
  return idList
    .map(id => components.value.find(c => String(c.ID) === String(id))?.Name)
    .filter(Boolean)
    .join(', ') || '—'
}
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('incidentTemplates.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template #actions>
        <OnyxButton
          v-if="canCreate"
          :label="t('incidentTemplates.newButton')"
          @click="openNew"
        />
      </template>
    </PageHeader>

    <!-- Richtung C: Liste und Formular nebeneinander (unter 860 px untereinander). -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('incidentTemplates.cardTitle')" class="split__main">
        <template #tools>
          <SegmentControl v-model="filter" :options="filterOptions" :label="t('incidentTemplates.filterLabel')" />
          <SearchBox v-model="query" :label="t('incidentTemplates.searchLabel')" :placeholder="t('incidentTemplates.searchPlaceholder')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('patterns.searchResult', { n: rows.length }, rows.length) }}</p>

        <ResponsiveTable v-if="rows.length" data-table :columns="columns" :items="rows" row-key="ID">
          <template #cell-Name="{ item }"><strong class="component-name__title">{{ item.Name }}</strong></template>
          <template #cell-Kind="{ item }"><span class="tag-pill">{{ kindLabel(item.Kind) }}</span></template>
          <template #cell-ComponentIDs="{ item }">{{ componentNames(item.ComponentIDs) }}</template>
          <template #actions="{ item }">
            <RowMenu :label="t('patterns.menuLabel', { name: item.Name })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="items.length" :title="t('incidentTemplates.noResults')" :text="t('incidentTemplates.noResultsText')" />
        <EmptyState v-else :title="t('incidentTemplates.empty')" icon="clipboard" />
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
          <FormLanguageSwitch :trans="trans" />
          <fieldset class="form-lock" :disabled="formReadOnly">

          <FormField v-slot="f" :label="t('incidentTemplates.fieldName')" required :error="errors.Name || null">
            <input :id="f.id" v-model="formData.Name" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('incidentTemplates.namePlaceholder')" @input="clearError('Name')">
          </FormField>

          <FormField v-slot="f" :label="t('incidentTemplates.fieldKind')" required>
            <select :id="f.id" v-model="formData.Kind" :required="f.required">
              <option v-for="o in kindOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </FormField>

          <FormField v-slot="f" :label="t('incidentTemplates.fieldTitle')" required :error="onDefaultLang ? errors.Title || null : null">
            <input :id="f.id" v-model="titleModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('incidentTemplates.titlePlaceholder')" @input="clearError('Title')">
          </FormField>

          <FormField v-slot="f" :label="t('incidentTemplates.fieldMessage')" required :error="onDefaultLang ? errors.Message || null : null">
            <RichTextEditor :id="f.id" v-model="messageModel" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('incidentTemplates.messagePlaceholder')" @input="clearError('Message')" />
          </FormField>

          <FormLanguageDone :trans="trans" />

          <FormField v-if="formData.Kind !== 'maintenance'" v-slot="f" :label="t('incidentTemplates.fieldDefaultStatus')">
            <select :id="f.id" v-model="formData.DefaultStatus">
              <option v-for="o in statusOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </FormField>

          <!-- Mehrfachauswahl im FormField-Muster (Label-Verknüpfung,
               Pflichtfeld-Fehler am Feld). -->
          <FormMultiSelect
            v-model="formData.ComponentIDs"
            :label="t('incidentTemplates.fieldComponents')"
            :options="componentOptions(components, formData.ComponentIDs)"
          />

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
