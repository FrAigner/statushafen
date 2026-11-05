<script setup>
/*
  Admin-Seite „Externe Quellen“ (Richtung C, Lagebild).
  Fremde Statusseiten (Summary-API v2) verwalten, ihre Bausteine und
  Gruppen mit eigenen Komponenten koppeln und die gespiegelten Vorfälle und
  Wartungen ansehen. Alles läuft gegen die vorhandene API unter
  /api/pages/:page_id/external-* (Lesen components:read, Ändern
  components:write); die Mandantentrennung erledigt das Backend, die Seite
  lädt nur für die aktive Statusseite und neu beim Mandantenwechsel.
  Texte der Fremdseite (Titel, Update-Texte, Fehler) werden nur als Text
  ausgegeben, nie als HTML (siehe white-space-Regel im Style).
*/
import { ref, computed, watch, nextTick } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi, isForbidden } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { useTagToggle, useAvailableTags } from '../../composables/useTagSelection'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useFormPanel } from '../../composables/useFormPanel'
import { useResourcePerms } from '../../composables/useResourcePerms'
import AppIcon from '../../components/AppIcon.vue'
import PageHeader from '../../components/PageHeader.vue'
import ListCard from '../../components/ListCard.vue'
import FormPanel from '../../components/FormPanel.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import FormField from '../../components/FormField.vue'
import KpiCard from '../../components/KpiCard.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import RowMenu from '../../components/RowMenu.vue'
import ListState from '../../components/ListState.vue'
import ForbiddenNotice from '../../components/ForbiddenNotice.vue'
import TagField from '../../components/TagField.vue'
import { vHelp } from '../../directives/vHelp'
import { flattenComponentTree, componentOptions } from '../../utils/componentTree'
import { formatRelative } from '../../utils/relativeTime'
import { normalizeStatus } from '../../utils/status'
import { confirmDialog } from '../../composables/useConfirm'

const { apiFetch } = useApi()
const { hasPerm } = useAuth()
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()
const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, activeTenantId)

const DEFAULT_POLL_SECONDS = 120
const DEFAULT_OUTAGE_MINUTES = 60
// Obergrenze des Backends pro Übernahme (handlers/external_import.go).
const MAX_IMPORT_ITEMS = 200

const sources = ref([])
const links = ref([])
const ownComponents = ref([])
const ownGroups = ref([])
const incidents = ref([])
const loading = ref(true)
const loadFailed = ref(false)
const incidentsFailed = ref(false)
const forbidden = ref(false)
const alertMsg = ref('')
const alertType = ref('warning')

// Rechte je Aktion (vorher ein gemeinsames components:write).
// Fetch ("Jetzt abrufen") ist Ändern; Übernehmen legt eigene Komponenten an und
// braucht deshalb zusätzlich components:create.
const { canCreate, canUpdate, canDelete } = useResourcePerms('external_sources')
const canImport = computed(() => canCreate.value && hasPerm('components:create'))
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const base = computed(() => `/api/pages/${activeTenantId.value}`)

const showAlert = (msg, type = 'warning') => {
  alertMsg.value = msg
  alertType.value = type
}

/* ── Seitenkopf ── */
const breadcrumbs = usePageCrumbs(() => t('externalSources.title'))

/* ── Kennzahlen ── */
const outageCount = computed(() => sources.value.filter(s => s.active && s.outage?.active).length)
const openIncidentCount = computed(() => incidents.value.filter(i => !i.closed).length)

/* ── Laden ── */
const loadIncidents = async () => {
  incidentsFailed.value = false
  const params = new URLSearchParams({ limit: '100' })
  if (filter.value.kind) params.set('kind', filter.value.kind)
  if (filter.value.state) params.set('state', filter.value.state)
  if (filter.value.sourceId) params.set('source_id', filter.value.sourceId)
  try {
    incidents.value = (await apiFetch(`${base.value}/external-incidents?${params}`)) || []
  } catch {
    incidentsFailed.value = true
  }
}

const loadData = async () => {
  forbidden.value = false
  loadFailed.value = false
  try {
    const [s, l, c] = await Promise.all([
      apiFetch(`${base.value}/external-sources`),
      apiFetch(`${base.value}/external-links`),
      apiFetch(`${base.value}/components`),
    ])
    sources.value = s || []
    links.value = l || []
    // Reine Gruppen lassen sich nicht koppeln (Backend: 400), nur ihre Kinder.
    const flat = flattenComponentTree(c)
    ownComponents.value = flat.filter(x => !x.IsGroup)
    // Zielgruppen der Übernahme (eingerückt nach Baumtiefe).
    ownGroups.value = flat.filter(x => x.IsGroup)
    // Tags nur für Global Admins (siehe useAvailableTags).
    await loadAvailableTags()
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
  if (!forbidden.value) await loadIncidents()
}

/* ── Quellenliste ── */
const intervalLabel = (seconds) => seconds % 60 === 0
  ? t('externalSources.everyMinutes', { n: seconds / 60 }, seconds / 60)
  : t('externalSources.everySeconds', { n: seconds }, seconds)

// Zustand einer Quelle als Abzeichen: Form + Farbe + Text (nie nur Farbe).
const sourceState = (s) => {
  if (!s.active) return { status: null, label: t('externalSources.stateInactive') }
  if (s.outage?.active) return { status: 'major', label: t('externalSources.stateOutage') }
  if (s.last_fetch_ok === false) return { status: 'degraded', label: t('externalSources.stateError') }
  if (s.last_fetch_ok === true) return { status: 'operational', label: t('externalSources.stateOk') }
  return { status: null, label: t('externalSources.stateNever') }
}

const sourceColumns = computed(() => [
  { key: 'name', label: t('externalSources.tableSource') },
  { key: 'state', label: t('externalSources.tableState') },
  { key: 'last_fetch_at', label: t('externalSources.tableLastFetch') },
  { key: 'components', label: t('externalSources.tableComponents'), numeric: true },
  { key: 'links', label: t('externalSources.tableLinks'), numeric: true },
])

const fetchingId = ref(null)
const deletingId = ref(null)

const fetchNow = async (src) => {
  fetchingId.value = src.ID
  try {
    const res = await apiFetch(`${base.value}/external-sources/${src.ID}/fetch`, { method: 'POST' })
    const r = res.result
    if (r.ok) showAlert(t('externalSources.fetchOk', { name: src.name }), 'success')
    else if (r.error === 'fetch_in_progress') showAlert(t('externalSources.fetchBusy', { name: src.name }), 'warning')
    else showAlert(t('externalSources.fetchFailed', { name: src.name, error: r.error }), 'error')
    await loadData()
  } catch (err) {
    showAlert(t('externalSources.fetchError') + err.message, 'error')
  } finally {
    fetchingId.value = null
  }
}

const deleteSource = async (src) => {
  if (!(await confirmDialog({ message: t('externalSources.confirmDelete', { name: src.name }) }))) return
  deletingId.value = src.ID
  try {
    await apiFetch(`${base.value}/external-sources/${src.ID}`, { method: 'DELETE' })
    if (String(linkSourceId.value) === String(src.ID)) linkSourceId.value = ''
    if (String(importSourceId.value) === String(src.ID)) importSourceId.value = ''
    await loadData()
  } catch (err) {
    showAlert(t('common.deleteError') + err.message, 'error')
  } finally {
    deletingId.value = null
  }
}

const menuItems = (s) => [
  ...(canUpdate.value ? [{ key: 'fetch', label: fetchingId.value === s.ID ? t('externalSources.fetching') : t('externalSources.fetchNow'), icon: 'activity', disabled: fetchingId.value === s.ID }] : []),
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === s.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === s.ID }] : []),
]
const onMenuSelect = (s, key) => {
  if (key === 'fetch') fetchNow(s)
  else if (key === 'edit') openEdit(s)
  else deleteSource(s)
}

/* ── Formular: Quelle anlegen und bearbeiten ── */
const isEditing = ref(false)
const currentEditId = ref(null)
const editName = ref('')
const formData = ref({})
// Fokus und Pflichtfeld-Fehler aus dem gemeinsamen Baustein.
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()
const isSaving = ref(false)

const formTitle = computed(() => currentEditId.value
  ? t('externalSources.editTitle', { name: editName.value })
  : t('externalSources.createTitle'))

const openNew = () => {
  formData.value = { name: '', base_url: '', poll_interval_seconds: DEFAULT_POLL_SECONDS, outage_after_minutes: DEFAULT_OUTAGE_MINUTES, active: true }
  currentEditId.value = null
  editName.value = ''
  clearErrors()
  isEditing.value = true
  focusForm()
}

const openEdit = (s) => {
  formData.value = {
    name: s.name,
    base_url: s.base_url,
    poll_interval_seconds: s.poll_interval_seconds,
    outage_after_minutes: s.outage_after_minutes,
    active: s.active,
  }
  currentEditId.value = s.ID
  editName.value = s.name
  clearErrors()
  isEditing.value = true
  focusForm()
}

const cancelEdit = () => {
  isEditing.value = false
  formData.value = {}
  currentEditId.value = null
  clearErrors()
}

// Pflichtfelder prüfen, bevor der Server gefragt wird; die Server-Prüfung
// (SSRF-Schutz, Wertebereiche) bleibt maßgeblich und wird als Meldung gezeigt.
const validate = () => validateRequired([
  { key: 'name', value: formData.value.name, message: t('externalSources.nameRequired') },
  { key: 'base_url', value: formData.value.base_url, message: t('externalSources.urlRequired') },
])

const saveSource = async () => {
  if (!(await validate())) return
  const isNew = currentEditId.value === null
  const payload = {
    name: formData.value.name,
    base_url: formData.value.base_url,
    poll_interval_seconds: Number(formData.value.poll_interval_seconds),
    outage_after_minutes: Number(formData.value.outage_after_minutes),
    active: !!formData.value.active,
  }
  isSaving.value = true
  try {
    await apiFetch(
      isNew ? `${base.value}/external-sources` : `${base.value}/external-sources/${currentEditId.value}`,
      { method: isNew ? 'POST' : 'PUT', body: JSON.stringify(payload) },
    )
    showAlert(isNew ? t('externalSources.created') : t('common.changesSaved'), 'success')
    cancelEdit()
    await loadData()
  } catch (err) {
    showAlert(t('common.saveError') + err.message, 'error')
  } finally {
    isSaving.value = false
  }
}

/* ── Kopplungen ── */
const linkComponentId = ref('')
const linkSourceId = ref('')
const linkExternalId = ref('')
const linkSearch = ref('')
const externalComponents = ref([])
const externalLoading = ref(false)
const externalFailed = ref(false)
const isLinking = ref(false)
const unlinkingId = ref(null)

const fetchExternalList = async (sourceId) => {
  const res = await apiFetch(`${base.value}/external-sources/${sourceId}/components`)
  return res?.components || []
}

const loadExternalComponents = async () => {
  linkExternalId.value = ''
  externalComponents.value = []
  externalFailed.value = false
  if (!linkSourceId.value) return
  externalLoading.value = true
  try {
    externalComponents.value = await fetchExternalList(linkSourceId.value)
  } catch {
    externalFailed.value = true
  } finally {
    externalLoading.value = false
  }
}
watch(linkSourceId, loadExternalComponents)

// Auswahl: ganze Gruppen zuerst als „Gruppe“ gekennzeichnet, dann die
// Bausteine mit ihrer Gruppe; die Suche filtert im Namen und im Gruppennamen.
const externalOptions = computed(() => {
  const needle = linkSearch.value.trim().toLowerCase()
  const match = (c) => !needle || `${c.name} ${c.group_name}`.toLowerCase().includes(needle)
  const label = (c) => c.is_group
    ? t('externalSources.optionGroup', { name: c.name })
    : (c.group_name ? `${c.group_name} › ${c.name}` : c.name)
  return externalComponents.value.filter(match).map(c => ({ value: String(c.ID), label: label(c) }))
})

// Hinweis unter einer Bausteinliste. Eine Quelle, die noch nie abgerufen wurde
// (oder inaktiv ist), hat keine Bausteine - das ist kein Ladefehler, sondern
// braucht erst einen Abruf bzw. die Aktivierung.
const listHint = (sourceId, { loading, failed, empty }) => {
  if (loading) return t('common.loading')
  const src = sources.value.find(x => String(x.ID) === String(sourceId))
  if (src && (failed || empty) && (!src.last_fetch_at || !src.component_count)) {
    return t(src.active ? 'externalSources.sourceNeverFetched' : 'externalSources.sourceInactive')
  }
  return failed ? t('common.listLoadError') : null
}
const linkListHint = computed(() => listHint(linkSourceId.value, {
  loading: externalLoading.value, failed: externalFailed.value, empty: !!linkSourceId.value && !externalComponents.value.length,
}))

const addLink = async () => {
  const e = {}
  if (!linkComponentId.value) e.linkComponent = t('externalSources.linkComponentRequired')
  if (!linkExternalId.value) e.linkExternal = t('externalSources.linkExternalRequired')
  linkErrors.value = e
  if (Object.keys(e).length) return
  isLinking.value = true
  try {
    await apiFetch(`${base.value}/external-links`, {
      method: 'POST',
      body: JSON.stringify({ component_id: Number(linkComponentId.value), external_component_id: Number(linkExternalId.value) }),
    })
    showAlert(t('externalSources.linked'), 'success')
    linkExternalId.value = ''
    await loadData()
  } catch (err) {
    showAlert(t('common.saveError') + err.message, 'error')
  } finally {
    isLinking.value = false
  }
}
const linkErrors = ref({})

/* ── Bausteine übernehmen ── */
const importSourceId = ref('')
const importItems = ref([])
const importLoading = ref(false)
const importFailed = ref(false)
const importSearch = ref('')
const importSelected = ref([])
const importTargetId = ref('')
const importForm = ref({ TagIDs: [] })
const toggleImportTag = useTagToggle(importForm)
const importError = ref('')
const isImporting = ref(false)
const importResult = ref(null)

const isImportLinked = (c) => {
  if (c.linked_component_ids?.length) return true
  if (!c.is_group) return false
  // Eine übernommene Gruppe wird selbst nicht gekoppelt, ihre Mitglieder schon.
  const members = importItems.value.filter(m => !m.is_group && m.group_external_id === c.external_id)
  return members.length > 0 && members.every(m => m.linked_component_ids?.length)
}

const visibleImportItems = computed(() => {
  const needle = importSearch.value.trim().toLowerCase()
  return importItems.value
    .filter(c => !needle || `${c.name} ${c.group_name}`.toLowerCase().includes(needle))
    .map(c => ({ ...c, key: String(c.ID), linked: isImportLinked(c) }))
})
const selectableImportKeys = computed(() => visibleImportItems.value.filter(c => !c.linked).map(c => c.key))

const groupTargetOptions = computed(() => [
  { value: '', label: t('externalSources.importTargetTop') },
  ...componentOptions(ownGroups.value),
])

const loadImportItems = async () => {
  importItems.value = []
  importSelected.value = []
  importFailed.value = false
  importError.value = ''
  if (!importSourceId.value) return
  importLoading.value = true
  try {
    importItems.value = await fetchExternalList(importSourceId.value)
  } catch {
    importFailed.value = true
  } finally {
    importLoading.value = false
  }
}
watch(importSourceId, () => {
  importResult.value = null
  loadImportItems()
})

const importListHint = computed(() => listHint(importSourceId.value, {
  loading: importLoading.value, failed: importFailed.value, empty: !!importSourceId.value && !importItems.value.length,
}))

const selectAllImport = () => {
  importSelected.value = [...new Set([...importSelected.value, ...selectableImportKeys.value])]
}
const clearImport = () => { importSelected.value = [] }

const listEl = ref(null)
const submitImport = async () => {
  importError.value = ''
  if (!importSourceId.value) {
    importError.value = t('externalSources.importSourceRequired')
    return
  }
  if (!importSelected.value.length) {
    importError.value = t('externalSources.importNothingSelected')
    await nextTick()
    listEl.value?.querySelector('input')?.focus()
    return
  }
  if (importSelected.value.length > MAX_IMPORT_ITEMS) {
    importError.value = t('externalSources.importTooMany', { max: MAX_IMPORT_ITEMS })
    return
  }
  const nameOf = (id) => importItems.value.find(c => c.ID === id)?.name || String(id)
  isImporting.value = true
  try {
    const res = await apiFetch(`${base.value}/external-sources/${importSourceId.value}/import`, {
      method: 'POST',
      body: JSON.stringify({
        external_component_ids: importSelected.value.map(Number),
        parent_id: importTargetId.value ? Number(importTargetId.value) : null,
        tag_ids: canSelectTags.value ? importForm.value.TagIDs : [],
      }),
    })
    const created = res?.created || []
    const skipped = (res?.skipped || []).map(x => ({ id: x.external_component_id, name: nameOf(x.external_component_id), reason: x.reason }))
    importResult.value = { created: created.length, skipped }
    showAlert(t('externalSources.importDone', { created: created.length, skipped: skipped.length }), 'success')
    await loadData()
    await loadImportItems()
  } catch (err) {
    importError.value = t('externalSources.importError') + err.message
  } finally {
    isImporting.value = false
  }
}
const importReason = (reason) => t(`externalSources.importReason.${reason}`)

const removeLink = async (link) => {
  if (!(await confirmDialog({ message: t('externalSources.confirmUnlink', { component: link.component_name, external: link.external_component_name }), confirmLabel: t('confirm.unlink') }))) return
  unlinkingId.value = link.id
  try {
    await apiFetch(`${base.value}/external-links/${link.id}`, { method: 'DELETE' })
    await loadData()
  } catch (err) {
    showAlert(t('common.deleteError') + err.message, 'error')
  } finally {
    unlinkingId.value = null
  }
}

const linkColumns = computed(() => [
  { key: 'component_name', label: t('externalSources.tableOwnComponent') },
  { key: 'external_component_name', label: t('externalSources.tableExternalComponent') },
  { key: 'source_name', label: t('externalSources.tableSource') },
  { key: 'status', label: t('externalSources.tableStatus') },
])

// Fremdstatus: bekannte Stufen als Abzeichen, alles andere neutral „unbekannt“;
// verschwundene Bausteine zählen nicht mehr und werden so gekennzeichnet.
const externalStatusLabel = (item) => {
  if (item.vanished) return t('externalSources.statusVanished')
  return normalizeStatus(item.status) ? undefined : t('externalSources.statusUnknown')
}

/* ── Gespiegelte Meldungen ── */
const filter = ref({ kind: '', state: '', sourceId: '' })
watch(filter, loadIncidents, { deep: true })

const incidentColumns = computed(() => [
  { key: 'title', label: t('externalSources.tableTitle') },
  { key: 'kind', label: t('externalSources.tableKind') },
  { key: 'status', label: t('externalSources.tableIncidentStatus') },
  { key: 'affected', label: t('externalSources.tableAffected') },
  { key: 'updated_at', label: t('externalSources.tableChanged') },
])
const incidentRows = computed(() => incidents.value.map(i => ({ ...i, key: i.id })))
const kindLabel = (kind) => t(kind === 'maintenance' ? 'externalSources.kindMaintenance' : 'externalSources.kindIncident')
const incidentStatusLabel = (i) => i.raw_status || i.status

// Laden der Seite beim Mandantenwechsel.
watch(activeTenantId, (id) => {
  if (!id) return
  loading.value = true
  linkSourceId.value = ''
  importSourceId.value = ''
  loadData()
}, { immediate: true })
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" data-help-exempt="Meldung schließen, selbsterklärend" :aria-label="t('common.dismissMessage')" @click="alertMsg = ''"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('externalSources.title')" :subtitle="forbidden ? null : t('externalSources.subtitle')" :breadcrumbs="breadcrumbs">
      <template #actions>
        <OnyxButton v-if="canCreate" v-help="'externalSources.newSource'" :label="t('externalSources.newButton')" @click="openNew" />
      </template>
    </PageHeader>

    <ForbiddenNotice v-if="forbidden" />

    <template v-else>
      <div class="kpi-grid">
        <KpiCard :label="t('externalSources.kpiSources')" :value="String(sources.length)" />
        <KpiCard :label="t('externalSources.kpiOutages')" :value="String(outageCount)" :alert="outageCount > 0" />
        <KpiCard :label="t('externalSources.kpiLinks')" :value="String(links.length)" />
        <KpiCard :label="t('externalSources.kpiOpen')" :value="String(openIncidentCount)" />
      </div>

      <div class="split" :class="{ 'split--open': isEditing }">
        <div class="split__main ext-stack">

          <!-- Quellen -->
          <ListCard :title="t('externalSources.cardSources')">
            <ResponsiveTable
              v-if="sources.length"
              data-table
              :columns="sourceColumns"
              :items="sources"
            >
              <template #cell-name="{ item: s }">
                <div class="ext-name">
                  <strong>{{ s.name }}</strong>
                  <small class="text-muted ext-text">{{ s.base_url }}</small>
                  <small class="text-muted">{{ t('externalSources.interval', { every: intervalLabel(s.poll_interval_seconds) }) }}</small>
                </div>
              </template>
              <template #cell-state="{ item: s }">
                <StatusBadge :status="sourceState(s).status" :label="sourceState(s).label" />
                <small v-if="s.outage?.active" class="ext-note">
                  {{ t('externalSources.outageSince', { when: formatRelative(s.outage.since, locale) }) }}
                </small>
                <small v-if="s.last_error && (s.outage?.active || s.last_fetch_ok === false)" class="ext-note ext-text">{{ s.last_error }}</small>
              </template>
              <template #cell-last_fetch_at="{ item: s }">
                <RelativeTime :value="s.last_fetch_at" />
                <small v-if="s.last_success_at && s.last_fetch_ok === false" class="ext-note">
                  {{ t('externalSources.lastSuccess', { when: formatRelative(s.last_success_at, locale) }) }}
                </small>
              </template>
              <template #cell-components="{ item: s }">{{ s.component_count }}</template>
              <template #cell-links="{ item: s }">{{ s.link_count }}</template>
              <template #actions="{ item: s }">
                <RowMenu
                  v-help="'externalSources.rowActions'"
                  :label="t('externalSources.menuLabel', { name: s.name })"
                  :items="menuItems(s)"
                  @select="key => onMenuSelect(s, key)"
                />
              </template>
            </ResponsiveTable>
            <ListState v-else :loading="loading" :error="loadFailed" :count="sources.length" :empty-text="t('externalSources.empty')" icon="globe" />
          </ListCard>

          <!-- Bausteine übernehmen -->
          <ListCard v-if="canImport && sources.length" :title="t('externalSources.cardImport')">
            <p class="ext-intro">{{ t('externalSources.importIntro') }}</p>

            <form class="ext-import" novalidate @submit.prevent="submitImport">
              <div class="ext-import__top">
                <FormField v-slot="f" :label="t('externalSources.importSourceLabel')" help="externalSources.importSource">
                  <select :id="f.id" v-model="importSourceId" :aria-describedby="f.describedby">
                    <option value="">{{ t('externalSources.choose') }}</option>
                    <option v-for="s in sources" :key="s.ID" :value="String(s.ID)">{{ s.name }}</option>
                  </select>
                </FormField>
                <FormField v-slot="f" :label="t('externalSources.importSearchLabel')" help="externalSources.importSearch">
                  <input :id="f.id" v-model="importSearch" type="search" :disabled="!importSourceId" :placeholder="t('externalSources.searchPlaceholder')" :aria-describedby="f.describedby">
                </FormField>
              </div>

              <p v-if="importListHint" class="ext-import__hint" role="status">
                <AppIcon name="info" :size="14" /> {{ importListHint }}
              </p>

              <fieldset v-if="importSourceId && importItems.length" ref="listEl" v-help="'externalSources.importList'" class="form-field form-field--set ext-import__list" role="group">
                <legend class="form-field__labelrow">
                  <span class="form-field__label">{{ t('externalSources.importListLabel') }}</span>
                  <span class="form-field__required" aria-hidden="true">*</span>
                </legend>
                <div class="ext-import__bulk">
                  <OnyxButton v-help="'externalSources.importSelectAll'" mode="outline" type="button" :label="t('externalSources.importSelectAll')" :disabled="!selectableImportKeys.length" @click="selectAllImport" />
                  <OnyxButton v-help="'externalSources.importClear'" mode="outline" type="button" :label="t('externalSources.importClear')" :disabled="!importSelected.length" @click="clearImport" />
                  <span class="ext-import__count" role="status">{{ t('externalSources.importSelected', { n: importSelected.length }) }}</span>
                </div>
                <ul class="ext-import__rows">
                  <li v-for="c in visibleImportItems" :key="c.key" class="ext-import__row" :class="{ 'ext-import__row--group': c.is_group }">
                    <label class="checkbox-row">
                      <input v-model="importSelected" type="checkbox" :value="c.key" :disabled="c.linked">
                      <span class="ext-name">
                        <strong>{{ c.name }}</strong>
                        <small v-if="c.is_group" class="text-muted">{{ t('externalSources.wholeGroup') }}</small>
                        <small v-else-if="c.group_name" class="text-muted">{{ c.group_name }}</small>
                      </span>
                    </label>
                    <span v-if="c.linked" class="badge badge-info">{{ t('externalSources.importLinked') }}</span>
                  </li>
                </ul>
                <p v-if="!visibleImportItems.length" class="ext-import__hint">{{ t('externalSources.importNoMatch') }}</p>
              </fieldset>

              <FormField v-slot="f" :label="t('externalSources.importTarget')" help="externalSources.importTarget">
                <select :id="f.id" v-model="importTargetId" :aria-describedby="f.describedby">
                  <option v-for="o in groupTargetOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
                </select>
              </FormField>
              <TagField v-if="canSelectTags" v-help="'externalSources.importTags'" :label="t('components.fieldTags')" :tags="availableTags" :selected-ids="importForm.TagIDs" @toggle="toggleImportTag" />

              <p v-if="importError" id="ext-import-error" class="form-field__message form-field__message--error ext-import__error" role="alert">
                <AppIcon name="alert-circle" :size="14" /> {{ importError }}
              </p>
              <div class="ext-import__actions">
                <OnyxButton v-help="'externalSources.importSubmit'" type="submit" :label="isImporting ? t('externalSources.importing') : t('externalSources.importButton', { n: importSelected.length })" :disabled="isImporting" />
              </div>

              <div v-if="importResult" class="ext-import__result" role="status">
                <p><strong>{{ t('externalSources.importDone', { created: importResult.created, skipped: importResult.skipped.length }) }}</strong></p>
                <ul v-if="importResult.skipped.length">
                  <li v-for="x in importResult.skipped" :key="x.id"><span class="ext-text">{{ x.name }}</span>: {{ importReason(x.reason) }}</li>
                </ul>
              </div>
            </form>
          </ListCard>

          <!-- Kopplungen -->
          <ListCard :title="t('externalSources.cardLinks')">
            <p class="ext-intro">{{ t('externalSources.linksIntro') }}</p>

            <form v-if="canCreate && sources.length" class="ext-link-form" novalidate @submit.prevent="addLink">
              <FormField v-slot="f" :label="t('externalSources.fieldOwnComponent')" required help="externalSources.linkComponent" :error="linkErrors.linkComponent || null">
                <select :id="f.id" v-model="linkComponentId" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @change="linkErrors = {}">
                  <option value="">{{ t('externalSources.choose') }}</option>
                  <option v-for="c in ownComponents" :key="c.ID" :value="String(c.ID)">{{ c.Name }}</option>
                </select>
              </FormField>
              <FormField v-slot="f" :label="t('externalSources.fieldSource')" help="externalSources.linkSource">
                <select :id="f.id" v-model="linkSourceId" :aria-describedby="f.describedby">
                  <option value="">{{ t('externalSources.choose') }}</option>
                  <option v-for="s in sources" :key="s.ID" :value="String(s.ID)">{{ s.name }}</option>
                </select>
              </FormField>
              <FormField v-slot="f" :label="t('externalSources.fieldSearch')" help="externalSources.linkSearch">
                <input :id="f.id" v-model="linkSearch" type="search" :disabled="!linkSourceId" :placeholder="t('externalSources.searchPlaceholder')" :aria-describedby="f.describedby">
              </FormField>
              <FormField v-slot="f" :label="t('externalSources.fieldExternal')" required :hint="linkListHint" help="externalSources.linkExternal" :error="linkErrors.linkExternal || null">
                <select :id="f.id" v-model="linkExternalId" :disabled="!linkSourceId" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @change="linkErrors = {}">
                  <option value="">{{ t('externalSources.choose') }}</option>
                  <option v-for="o in externalOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
                </select>
              </FormField>
              <div class="ext-link-form__actions">
                <!-- Unsichtbare Beschriftung: setzt den Knopf auf die Höhe der Eingabefelder,
                     auch wenn ein Feld darunter einen Hinweis zeigt. -->
                <span class="form-field__label ext-link-form__spacer" aria-hidden="true">&nbsp;</span>
                <OnyxButton v-help="'externalSources.linkAdd'" :label="isLinking ? t('common.adding') : t('externalSources.linkButton')" type="submit" :disabled="isLinking" />
              </div>
            </form>

            <ResponsiveTable v-if="links.length" data-table :columns="linkColumns" :items="links">
              <template #cell-external_component_name="{ item: l }">
                <div class="ext-name">
                  <strong>{{ l.external_component_name }}</strong>
                  <small v-if="l.is_group" class="text-muted">{{ t('externalSources.wholeGroup') }}</small>
                  <small v-else-if="l.group_name" class="text-muted">{{ l.group_name }}</small>
                  <small v-if="l.warnings.includes('overlaps_group')" class="ext-warning">
                    <AppIcon name="alert-triangle" :size="12" /> {{ t('externalSources.warnOverlap') }}
                  </small>
                  <small v-if="l.is_group && l.worst" class="text-muted">{{ t('externalSources.worstMember', { name: l.worst.name }) }}</small>
                </div>
              </template>
              <template #cell-status="{ item: l }"><StatusBadge :status="l.vanished ? null : l.status" :label="externalStatusLabel(l)" /></template>
              <template v-if="canDelete" #actions="{ item: l }">
                <OnyxButton
                  v-help="'externalSources.unlink'"
                  mode="outline"
                  :label="unlinkingId === l.id ? t('common.removing') : t('externalSources.unlinkButton')"
                  :disabled="unlinkingId === l.id"
                  @click="removeLink(l)"
                />
              </template>
            </ResponsiveTable>
            <ListState v-else :loading="loading" :error="loadFailed" :count="links.length" :empty-text="t('externalSources.linksEmpty')" icon="globe" />
          </ListCard>

          <!-- Gespiegelte Meldungen -->
          <ListCard :title="t('externalSources.cardIncidents')">
            <p class="ext-intro">{{ t('externalSources.incidentsIntro') }}</p>
            <div class="ext-filters">
              <FormField v-slot="f" :label="t('externalSources.filterKind')" help="externalSources.filterKind">
                <select :id="f.id" v-model="filter.kind" :aria-describedby="f.describedby">
                  <option value="">{{ t('common.all') }}</option>
                  <option value="incident">{{ t('externalSources.kindIncident') }}</option>
                  <option value="maintenance">{{ t('externalSources.kindMaintenance') }}</option>
                </select>
              </FormField>
              <FormField v-slot="f" :label="t('externalSources.filterState')" help="externalSources.filterState">
                <select :id="f.id" v-model="filter.state" :aria-describedby="f.describedby">
                  <option value="">{{ t('common.all') }}</option>
                  <option value="open">{{ t('externalSources.stateOpen') }}</option>
                  <option value="closed">{{ t('externalSources.stateClosed') }}</option>
                </select>
              </FormField>
              <FormField v-slot="f" :label="t('externalSources.fieldSource')" help="externalSources.filterSource">
                <select :id="f.id" v-model="filter.sourceId" :aria-describedby="f.describedby">
                  <option value="">{{ t('common.all') }}</option>
                  <option v-for="s in sources" :key="s.ID" :value="String(s.ID)">{{ s.name }}</option>
                </select>
              </FormField>
            </div>
            <ResponsiveTable v-if="incidentRows.length" data-table row-key="key" :columns="incidentColumns" :items="incidentRows">
              <template #cell-title="{ item: i }">
                <div class="ext-name">
                  <a v-if="i.url" :href="i.url" target="_blank" rel="noopener noreferrer"><strong class="ext-text">{{ i.title }}</strong></a>
                  <strong v-else class="ext-text">{{ i.title }}</strong>
                  <small class="text-muted">{{ i.source_name }}</small>
                  <details v-if="i.updates.length" class="ext-updates">
                    <summary>{{ t('externalSources.updates', { n: i.updates.length }, i.updates.length) }}</summary>
                    <ol>
                      <li v-for="u in i.updates" :key="u.id">
                        <time :datetime="u.display_at">{{ new Date(u.display_at).toLocaleString(locale) }}</time>
                        <span class="ext-text">{{ u.body }}</span>
                      </li>
                    </ol>
                  </details>
                </div>
              </template>
              <template #cell-kind="{ item: i }">{{ kindLabel(i.kind) }}</template>
              <template #cell-status="{ item: i }">
                {{ i.closed ? t('externalSources.stateClosed') : t('externalSources.stateOpen') }}
                <small class="cell-sub ext-text" data-testid="ext-raw-status">{{ incidentStatusLabel(i) }}</small>
              </template>
              <template #cell-affected="{ item: i }">{{ i.affected_components.map(c => c.name).join(', ') }}</template>
              <template #cell-updated_at="{ item: i }">
                <RelativeTime :value="i.updated_at" />
              </template>
            </ResponsiveTable>
            <ListState v-else :loading="loading" :error="incidentsFailed" :count="incidentRows.length" :empty-text="t('externalSources.incidentsEmpty')" icon="alert-circle" />
          </ListCard>
        </div>

        <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
          <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveSource">
            <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
            <fieldset class="form-lock" :disabled="formReadOnly">
            <FormField v-slot="f" :label="t('externalSources.fieldName')" required help="externalSources.name" :error="errors.name || null">
              <input :id="f.id" v-model="formData.name" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('externalSources.namePlaceholder')" @input="clearError('name')">
            </FormField>
            <FormField v-slot="f" :label="t('externalSources.fieldUrl')" required :hint="t('externalSources.urlHint')" help="externalSources.baseUrl" :error="errors.base_url || null">
              <input :id="f.id" v-model="formData.base_url" type="url" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" placeholder="https://status.example.com" @input="clearError('base_url')">
            </FormField>
            <div class="form-grid">
              <FormField v-slot="f" :label="t('externalSources.fieldInterval')" help="externalSources.interval">
                <input :id="f.id" v-model.number="formData.poll_interval_seconds" type="number" min="60" max="86400" step="30" :aria-describedby="f.describedby">
              </FormField>
              <FormField v-slot="f" :label="t('externalSources.fieldOutage')" help="externalSources.outageAfter">
                <input :id="f.id" v-model.number="formData.outage_after_minutes" type="number" min="10" max="1440" :aria-describedby="f.describedby">
              </FormField>
            </div>
            <FormField v-slot="f" :label="t('externalSources.fieldActive')" help="externalSources.active">
              <label class="checkbox-row"><input :id="f.id" v-model="formData.active" type="checkbox" :aria-describedby="f.describedby"> {{ t('externalSources.activeLabel') }}</label>
            </FormField>
            </fieldset>
            <div class="form-actions">
              <OnyxButton v-help="'externalSources.cancel'" :label="t('common.cancel')" mode="outline" type="button" :disabled="isSaving" @click="cancelEdit" />
              <OnyxButton v-if="!formReadOnly" v-help="'externalSources.save'" :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
            </div>
          </form>
        </FormPanel>
      </div>
    </template>
  </div>
</template>

<style scoped>
.kpi-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: var(--sp-4);
  margin-bottom: var(--sp-5);
}
@media (min-width: 640px) { .kpi-grid { grid-template-columns: repeat(2, 1fr); } }
@media (min-width: 1024px) { .kpi-grid { grid-template-columns: repeat(4, 1fr); } }
.ext-stack { display: flex; flex-direction: column; gap: var(--sp-5); }
.ext-intro { margin: 0; padding: 0 var(--sp-4) var(--sp-3); color: var(--text-2); font-size: var(--fs-sm); }
.ext-name { display: flex; flex-direction: column; gap: 2px; min-inline-size: 0; }
/* Texte der Fremdseite: nur Text, Umbrüche bleiben, lange URLs brechen um. */
.ext-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.ext-note { display: block; margin-top: 2px; color: var(--text-2); font-size: var(--fs-xs); }
.ext-warning { display: inline-flex; align-items: center; gap: 4px; color: var(--deg-text); font-size: var(--fs-xs); }
.ext-link-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--sp-3) var(--sp-4);
  align-items: start;
  padding: 0 var(--sp-4) var(--sp-4);
}
.ext-link-form__actions { display: flex; flex-direction: column; gap: var(--sp-1); }
.ext-link-form__spacer { line-height: 1.4; }
/* Knopf so hoch wie die Eingabefelder (--ctl-h), sonst steht er ein paar Pixel tiefer. */
.ext-link-form__actions :deep(button) { block-size: var(--ctl-h); min-block-size: var(--ctl-h); }
.ext-filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: var(--sp-3) var(--sp-4);
  padding: 0 var(--sp-4) var(--sp-4);
}
.ext-updates { margin-top: var(--sp-1); font-size: var(--fs-sm); }
.ext-updates ol { margin: var(--sp-2) 0 0; padding-inline-start: var(--sp-5); }
.ext-updates li { display: flex; flex-direction: column; gap: 2px; margin-bottom: var(--sp-2); }
.ext-updates time { color: var(--text-2); font-size: var(--fs-xs); }
.ext-import { padding: 0 var(--sp-4) var(--sp-4); }
.ext-import__top { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: var(--sp-3) var(--sp-4); }
.ext-import__hint { display: flex; align-items: flex-start; gap: 6px; margin: 0 0 var(--sp-4); color: var(--text-2); font-size: var(--fs-sm); }
.ext-import__bulk { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-2); margin-bottom: var(--sp-2); }
.ext-import__count { color: var(--text-2); font-size: var(--fs-sm); }
.ext-import__rows { list-style: none; margin: 0; padding: 0; max-block-size: 320px; overflow-y: auto; border: 1px solid var(--border); border-radius: var(--radius-md); }
.ext-import__row { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-3); padding: var(--sp-2) var(--sp-3); border-block-end: 1px solid var(--border); }
.ext-import__row:last-child { border-block-end: 0; }
.ext-import__row--group { background: var(--primary-soft); }
.ext-import__error { margin-bottom: var(--sp-3); }
.ext-import__actions { display: flex; justify-content: flex-end; }
.ext-import__result { margin-top: var(--sp-3); padding: var(--sp-3); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--surface-2); font-size: var(--fs-sm); }
.ext-import__result p { margin: 0 0 var(--sp-2); }
.ext-import__result ul { margin: 0; padding-inline-start: var(--sp-5); }
</style>
