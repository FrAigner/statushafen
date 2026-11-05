<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { OnyxButton, OnyxSwitch } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi, isForbidden } from '../../composables/useApi'
import { useResourcePerms } from '../../composables/useResourcePerms'
import { useTenant } from '../../composables/useTenant'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useTagToggle, useAvailableTags } from '../../composables/useTagSelection'
import { useSaveAction, useDeleteAction } from '../../composables/useAdminCrudActions'
import { useFormTranslations } from '../../composables/useFormTranslations'
import FormLanguageSwitch from '../../components/FormLanguageSwitch.vue'
import FormLanguageDone from '../../components/FormLanguageDone.vue'
import AppIcon from '../../components/AppIcon.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import ListCard from '../../components/ListCard.vue'
import FormPanel from '../../components/FormPanel.vue'
import SearchBox from '../../components/SearchBox.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import QuickStatus from '../../components/QuickStatus.vue'
import SegmentControl from '../../components/SegmentControl.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import StatusTiles from '../../components/StatusTiles.vue'
import TagChips from '../../components/TagChips.vue'
import RowMenu from '../../components/RowMenu.vue'
import EmptyState from '../../components/EmptyState.vue'
import ForbiddenNotice from '../../components/ForbiddenNotice.vue'
import { vHelp } from '../../directives/vHelp'
import { flattenComponentTree, descendantIds, visibleComponents, componentOptions } from '../../utils/componentTree'
import { formatRelative } from '../../utils/relativeTime'

const { apiFetch } = useApi()
// Rechte dieses Tabs je Aktion. Ohne Ändern öffnet sich ein bestehender
// Eintrag schreibgeschützt (formReadOnly), ohne Löschen entfällt der Eintrag im Zeilenmenü.
const { canCreate, canUpdate, canDelete } = useResourcePerms('components')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()
// Hilfetexte (help.components.*) für jedes Feld und jeden Button
// per v-help (erscheint nach Verweilen, abschaltbar pro Benutzer).

const items          = ref([])
const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, activeTenantId)
const isEditing      = ref(false)
const currentEditId  = ref(null)
const editName       = ref('')
const formData       = ref({})

// Sprachumschalter über Name und Beschreibung (weitere Sprachen der
// Seite). Die Beschreibung ist optional, eine Übersetzung braucht nur den Namen.
// Übersetzungen folgen dem Recht „Ändern“ des Tabs.
const trans = useFormTranslations({
  apiFetch,
  urls: { list: (id) => `/api/components/${id}/translations/all`, save: (id, code) => `/api/components/${id}/translations/${code}` },
  contentField: 'description',
  formData,
  canEdit: canUpdate,
  titleKey: 'Name',
  contentKey: 'Description',
  contentRequired: false,
})
const { showSwitcher, onDefaultLang, titleModel, contentModel: descriptionModel, active: activeLang } = trans
const formEl         = ref(null)
const nameError      = ref('')
const descriptionError = ref('')
const slugError      = ref('')
const forbidden      = ref(false)
const alertMsg       = ref('')
const alertType      = ref('warning')
const isSaving       = ref(false)
const deletingId     = ref(null)
const query          = ref('')
const collapsed      = ref(new Set())

// Richtung C: Der Titel nennt den Namen, nicht die ID.
const formTitle = computed(() => {
  if (currentEditId.value) return t(formData.value.IsGroup ? 'components.editGroupTitle' : 'components.editTitle', { name: editName.value })
  return t(formData.value.IsGroup ? 'components.createGroupTitle' : 'components.createTitle')
})

/* ── Feldlängen ── */
// Grenzen wie im Backend (handlers/component.go); gezählt wird in Zeichen
// (Code Points), nicht in UTF-16-Einheiten, damit Umlaute und Emojis wie dort
// je als eins zählen.
const NAME_MAX = 60
const DESCRIPTION_MAX = 240
// Kennung, Format und Länge wie im Backend (models/component_slug.go).
const SLUG_MAX = 60
const SLUG_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/
const charCount = (value) => [...(value || '')].length
const nameCount = computed(() => charCount(formData.value.Name))
const descriptionCount = computed(() => charCount(formData.value.Description))
// Angezeigt wird der Zähler des Feldes im aktiven Sprachreiter.
const shownNameCount = computed(() => charCount(titleModel.value))
const shownDescriptionCount = computed(() => charCount(descriptionModel.value))
// Bestehende Texte können länger sein als erlaubt (vor der Umstellung gespeichert): Sie
// werden nicht gekürzt, das Feld meldet den Überhang aber sofort, damit das
// nächste Speichern nicht überrascht.
const nameFieldError = computed(() => nameError.value || (nameCount.value > NAME_MAX ? t('components.nameTooLong', { max: NAME_MAX }) : null))
// Die Kennung gibt es nur beim Anlegen vorzugeben (danach schreibgeschützt).
const slugValue = computed(() => (formData.value.Slug || '').trim())
const slugFieldError = computed(() => slugError.value || (currentEditId.value === null && slugValue.value && (slugValue.value.length > SLUG_MAX || !SLUG_RE.test(slugValue.value)) ? t('components.slugInvalid', { max: SLUG_MAX }) : null))
const descriptionFieldError = computed(() => descriptionError.value || (descriptionCount.value > DESCRIPTION_MAX ? t('components.descriptionTooLong', { max: DESCRIPTION_MAX }) : null))

/* ── Seitenkopf: Brotkrumen und Untertitel ── */
const breadcrumbs = usePageCrumbs(() => t('components.title'))
// Aktiv/Archiviert. Archivierte Einträge (ArchivedAt) stehen nur im
// Segment „Archiviert“ und zählen nicht in den Untertitel.
const isArchived = c => !!c.ArchivedAt
const activeItems = computed(() => items.value.filter(c => !isArchived(c)))
const archivedItems = computed(() => items.value.filter(isArchived))
const view = ref('active')
const viewOptions = computed(() => [
  { value: 'active', label: t('components.viewActive', { n: activeItems.value.length }) },
  { value: 'archived', label: t('components.viewArchived', { n: archivedItems.value.length }) },
])
const archivedView = computed(() => view.value === 'archived')
// Tiefe neu bestimmen: Ein einzeln archiviertes Mitglied einer aktiven Gruppe
// stünde sonst eingerückt ohne Gruppe in der Liste.
const withShownDepth = (list) => {
  const depthOf = new Map()
  return list.map(c => {
    const depth = depthOf.has(c.ParentID) ? depthOf.get(c.ParentID) + 1 : 0
    depthOf.set(c.ID, depth)
    return { ...c, depth }
  })
}
const shownItems = computed(() => archivedView.value ? withShownDepth(archivedItems.value) : activeItems.value)
const groupCount = computed(() => activeItems.value.filter(c => c.IsGroup).length)
const componentCount = computed(() => activeItems.value.length - groupCount.value)
const subtitle = computed(() => t('components.subtitle', {
  components: t('components.countComponents', { n: componentCount.value }, componentCount.value),
  groups: t('components.countGroups', { n: groupCount.value }, groupCount.value),
}))

/* ── Liste: Suche und einklappbare Gruppen ── */
const searchText = c => `${c.Name} ${c.Slug || ''} ${c.Description || ''} ${(c.Tags || []).map(tag => tag.Name).join(' ')}`
const rows = computed(() => visibleComponents(shownItems.value, { query: query.value, collapsed: collapsed.value, textOf: searchText }))
const searching = computed(() => query.value.trim() !== '')

const toggleGroup = (id) => {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
}

/* ── Formular: Gruppe (Eltern-Komponente) ── */
// Als Gruppe wählbar sind Gruppen, außer der Komponente selbst und ihren
// Nachfahren (das Backend lehnt Zyklen ab). Ist die aktuelle Eltern-
// Komponente keine Gruppe, bleibt sie trotzdem wählbar, sonst ginge sie beim
// Speichern verloren.
const parentOptions = computed(() => {
  const excluded = new Set(currentEditId.value ? [currentEditId.value, ...descendantIds(items.value, currentEditId.value)] : [])
  const current = formData.value.ParentID
  const candidates = items.value.filter(c => !excluded.has(c.ID) && (c.IsGroup || c.ID === current))
  return [{ label: t('components.groupNone'), value: null }, ...componentOptions(candidates, [current]).map(o => ({ ...o, value: Number(o.value) }))]
})

/* ── Data loading ── */
const loadData = async () => {
  forbidden.value = false
  try {
    // Die API liefert einen Baum. Flach in Baumreihenfolge mit
    // Tiefe, damit auch Unterkomponenten gelistet und bearbeitbar sind.
    items.value = flattenComponentTree(await apiFetch(`/api/pages/${activeTenantId.value}/components`))
  } catch (err) {
    // 403 -> Berechtigungsmeldung statt Ladefehler-Alert.
    if (isForbidden(err)) forbidden.value = true
    else showAlert(t('common.loadError') + err.message, 'error')
  }
  // Tags nur für Global Admins (siehe useAvailableTags).
  await loadAvailableTags()
  // weitere Sprachen der Seite für den Umschalter.
  await trans.load(activeTenantId.value)
}

const toggleTag = useTagToggle(formData)

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

// Formular öffnen: Fokus auf das Namensfeld; unter 860 px liegt das Formular
// unter der Liste und wird in den Sichtbereich geholt.
const focusForm = async () => {
  await nextTick()
  formEl.value?.closest('aside')?.scrollIntoView?.({ block: 'nearest' })
  formEl.value?.querySelector('input')?.focus({ preventScroll: true })
}

// isGroup legt eine Gruppe an (kein Status, wird aus den Mitgliedern berechnet).
const openNew = (isGroup = false) => {
  formData.value  = isGroup
    ? { IsGroup: true, TagIDs: [], ParentID: null, Visible: true }
    : { Status: 'operational', TagIDs: [], ParentID: null, Visible: true }
  currentEditId.value = null
  editName.value = ''
  nameError.value = ''
  descriptionError.value = ''
  slugError.value = ''
  trans.reset()
  isEditing.value = true
  focusForm()
}

const openEdit = (item) => {
  // Visible fehlt nur bei Altständen der API: dann gilt sichtbar.
  formData.value  = { ...item, TagIDs: (item.Tags || []).map(t => t.ID), Visible: item.Visible !== false }
  currentEditId.value = item.ID
  editName.value = item.Name
  nameError.value = ''
  descriptionError.value = ''
  slugError.value = ''
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
  nameError.value = ''
  descriptionError.value = ''
  slugError.value = ''
}

// Längenfehler des Servers (Race mit anderem Client, ältere
// Frontend-Stände) landen am Feld statt als allgemeine Meldung.
const onSaveError = (err) => {
  if (err.data?.code === 'slug_invalid') slugError.value = t('components.slugInvalid', { max: SLUG_MAX })
  else if (err.data?.code === 'slug_taken') slugError.value = t('components.slugTaken')
  else if (err.data?.code === 'name_too_long') nameError.value = t('components.nameTooLong', { max: NAME_MAX })
  else if (err.data?.code === 'description_too_long') descriptionError.value = t('components.descriptionTooLong', { max: DESCRIPTION_MAX })
  else showAlert(t('common.saveError') + err.message, 'error')
}
const saveAction = useSaveAction({ apiFetch, isSaving, reload: loadData, onStart: clearAlert, onError: onSaveError })
// 409 group_not_empty (Gruppe wurde inzwischen befüllt) mit
// verständlicher, übersetzter Meldung statt der englischen Server-Meldung.
const deleteAction = useDeleteAction({
  apiFetch, deletingId, reload: loadData, onStart: clearAlert,
  onDeleted: () => showAlert(t('adminFeedback.deleted'), 'success'),
  onError: (err) => showAlert(err.data?.code === 'group_not_empty'
    ? t('components.groupNotEmpty')
    : t('common.deleteError') + err.message, 'error'),
})

const saveItem = async () => {
  // Pflichtfeld Name: Fehler am Feld statt eines Serverfehlers, Fokus aufs Feld.
  if (!(formData.value.Name || '').trim()) nameError.value = t('components.nameRequired')
  if (nameFieldError.value || descriptionFieldError.value || slugFieldError.value) {
    activeLang.value = '' // die Prüfungen gehören zur Standardsprache
    await nextTick()
    formEl.value?.querySelector('[aria-invalid="true"]')?.focus()
    return
  }
  if (!trans.checkComplete(showAlert)) return
  const isNew = currentEditId.value === null
  const url   = isNew
    ? `/api/pages/${activeTenantId.value}/components`
    : `/api/pages/${activeTenantId.value}/components/${currentEditId.value}`
  // Payload explizit gebaut statt formData direkt zu serialisieren: der
  // Backend-Request-DTO erwartet "tag_ids" (snake_case), formData.TagIDs
  // würde als JSON-Key "TagIDs" ankommen, was NICHT case-insensitive auf
  // "tag_ids" matcht (anders als z.B. "Name"/"name").
  const payload = {
    name: formData.value.Name,
    description: formData.value.Description,
    tag_ids: formData.value.TagIDs || [],
    // 0 = oberste Ebene (beim Bearbeiten löst das die Zuordnung).
    parent_id: formData.value.ParentID || 0,
    // „Auf der Statusseite anzeigen“.
    visible: formData.value.Visible !== false,
  }
  // Bei Gruppen ist Status der aus den Kindern berechnete Wert.
  // Zurückgeschickt hätte er die Gruppe zur normalen Komponente gemacht.
  if (!formData.value.IsGroup) payload.status = formData.value.Status
  // Neue Gruppe ohne Status, dafür mit is_group.
  else if (isNew) payload.is_group = true
  // Kennung nur beim Anlegen senden; leer = automatisch.
  if (isNew && slugValue.value) payload.slug = slugValue.value
  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved) => {
    // Neue ID merken, bevor Übersetzungen folgen: scheitert eine, legt ein zweiter Versuch keine zweite Komponente an.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value)
    const createdKey = formData.value.IsGroup ? 'components.groupCreated' : 'components.created'
    showAlert(isNew ? t(createdKey) : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id, name) =>
  deleteAction(`/api/pages/${activeTenantId.value}/components/${id}`, id, t('components.confirmDelete', { name }))

// Zeilenmenü: Einträge je Zeile; „Übersetzungen“ ist ein Link.
// Eine Gruppe mit Mitgliedern lässt sich nicht löschen (Backend: 409).
const isNonEmptyGroup = (c) => !!c.IsGroup && items.value.some(x => x.ParentID === c.ID)
const deleteLabel = (c) => {
  if (deletingId.value === c.ID) return t('common.deleting')
  return isNonEmptyGroup(c) ? t('components.deleteGroupBlocked') : t('common.delete')
}
const menuItems = (c) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  { key: 'translations', label: t('translations.manageLink'), icon: 'globe', to: `/admin/components/${c.ID}/translations` },
  ...(canUpdate.value ? [isArchived(c)
    ? { key: 'restore', label: t('components.restore'), icon: 'arrow-left' }
    : { key: 'archive', label: t('components.archive'), icon: 'folder' }] : []),
  ...(canDelete.value ? [{ key: 'delete', label: deleteLabel(c), icon: 'trash', danger: true, disabled: deletingId.value === c.ID || isNonEmptyGroup(c) }] : []),
]
const onMenuSelect = (c, key) => {
  if (key === 'edit') openEdit(c)
  else if (key === 'archive' || key === 'restore') setArchived(c, key)
  else deleteItem(c.ID, c.Name) // „Übersetzungen“ ist ein Link und meldet nichts
}

// Archivieren/Wiederherstellen über eigene Routen; die Liste lädt
// neu, weil eine Gruppe ihre Mitglieder mitnimmt.
const setArchived = async (comp, action) => {
  clearAlert()
  try {
    await apiFetch(`/api/pages/${activeTenantId.value}/components/${comp.ID}/${action}`, { method: 'POST' })
    showAlert(t(action === 'archive' ? 'components.archived' : 'components.restored'), 'success')
    await loadData()
  } catch (err) {
    showAlert(err.data?.code === 'parent_archived'
      ? t('components.parentArchived')
      : t(action === 'archive' ? 'components.archiveError' : 'components.restoreError') + err.message, 'error')
  }
}

const quickStatus = async (comp, newStatus) => {
  try {
    await apiFetch(`/api/pages/${activeTenantId.value}/components/${comp.ID}`, {
      method: 'PUT',
      // nur den Status senden; fehlende Felder lässt das Backend
      // unverändert. Die ganze Zeile hätte jetzt auch Children und depth
      // mitgeschickt.
      body: JSON.stringify({ status: newStatus })
    })
    // Rückmeldung wie beim Formular, statt eine ältere Meldung
    // (z. B. "Komponente erstellt.") stehen zu lassen.
    showAlert(t('common.changesSaved'), 'success')
    loadData()
  } catch (err) {
    showAlert(t('components.quickStatusError') + err.message, 'error')
  }
}
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" data-help-exempt="Meldung schließen, selbsterklärend" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('components.title')" :subtitle="forbidden ? null : subtitle" :breadcrumbs="breadcrumbs">
      <template #actions>
        <OnyxButton
          v-if="canCreate"
          v-help="'components.newComponent'"
          :label="t('components.newButton')"
          @click="openNew()"
        />
        <OnyxButton
          v-if="canCreate"
          v-help="'components.newGroup'"
          :label="t('components.newGroupButton')"
          mode="outline"
          @click="openNew(true)"
        />
      </template>
    </PageHeader>

    <ForbiddenNotice v-if="forbidden" />

    <!-- Stufe 4c: Liste und Formular stehen nebeneinander
         (unter 860 px untereinander), das Formular ersetzt die Liste nicht mehr. -->
    <div v-else class="split" :class="{ 'split--open': isEditing }">
      <ListCard class="split__main" :title="t('components.cardTitle')">
        <template #tools>
          <SearchBox
            v-model="query"
            v-help="'components.search'"
            :label="t('components.searchLabel')"
            :placeholder="t('components.searchPlaceholder')"
          />
          <SegmentControl v-model="view" :options="viewOptions" :label="t('components.viewLabel')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('components.searchResult', { n: rows.length }, rows.length) }}</p>

        <div class="data-table-wrapper comp-card__table">
          <table v-if="rows.length" class="data-table data-table--cards">
            <thead>
              <tr>
                <th scope="col">{{ t('components.tableName') }}</th>
                <th scope="col">{{ t('components.tableTags') }}</th>
                <th scope="col">{{ t('components.tableStatus') }} <span class="th-hint">{{ t('components.tableStatusHint') }}</span></th>
                <th scope="col">{{ t('components.tableChanged') }}</th>
                <th scope="col" class="actions-col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <!-- Gruppen (IsGroup) sind einklappbare Gruppenzeilen. -->
              <tr v-for="c in rows" :key="c.ID" :class="{ 'data-table__group': c.IsGroup }">
                <component :is="c.IsGroup ? 'th' : 'td'" :scope="c.IsGroup ? 'row' : undefined" :data-label="t('components.tableName')">
                  <!-- Unterkomponenten nach Tiefe im Baum eingerückt. -->
                  <div class="component-name" :class="`depth-${Math.min(c.depth, 6)}`" :data-depth="c.depth">
                    <button
                      v-if="c.IsGroup"
                      type="button"
                      class="group-toggle"
                      :aria-expanded="collapsed.has(c.ID) && !searching ? 'false' : 'true'"
                      :aria-label="t(collapsed.has(c.ID) ? 'components.groupExpand' : 'components.groupCollapse', { name: c.Name })"
                      data-help-exempt="Gruppe ein- oder ausklappen, selbsterklärend"
                      @click="toggleGroup(c.ID)"
                    >
                      <AppIcon :name="collapsed.has(c.ID) && !searching ? 'chevron-right' : 'chevron-down'" :size="14" />
                    </button>
                    <AppIcon v-if="c.IsGroup" name="folder" :size="16" class="group-icon" />
                    <strong class="component-name__title">{{ c.Name }}</strong>
                    <span v-if="c.IsGroup" class="group-count" :aria-label="t('components.groupCount', { n: c.Children?.length ?? 0 }, c.Children?.length ?? 0)">{{ c.Children?.length ?? 0 }}</span>
                    <span v-if="c.Visible === false" class="tag-pill" :title="t('components.hiddenTitle')"><AppIcon name="eye-off" :size="12" /> {{ t('components.hiddenBadge') }}</span>
                    <code v-if="c.Slug" class="component-name__slug">{{ c.Slug }}</code>
                    <small v-if="c.Description" class="component-name__desc">{{ c.Description }}</small>
                  </div>
                </component>
                <td :data-label="t('components.tableTags')">
                  <span v-if="!(c.Tags || []).length" class="text-muted">—</span>
                  <span v-else class="tag-pills"><span v-for="tag in c.Tags" :key="tag.ID" class="tag-pill">{{ tag.Name }}</span></span>
                </td>
                <td :data-label="t('components.tableStatus')">
                  <QuickStatus
                    v-if="canUpdate && !c.IsGroup && !archivedView"
                    v-help="'components.quickStatus'"
                    :model-value="c.Status"
                    :label="t('components.quickStatusAria', { name: c.Name })"
                    @update:model-value="status => quickStatus(c, status)"
                  />
                  <StatusBadge v-else-if="c.Status !== 'group'" :status="c.Status" />
                  <!-- Gruppen zeigen den berechneten Status nur an. -->
                  <span v-if="c.IsGroup" class="sr-only">{{ t('components.groupLabel') }}</span>
                </td>
                <td :data-label="t('components.tableChanged')" class="cell-changed">
                  <RelativeTime :value="c.UpdatedAt" />
                </td>
                <td class="actions-col">
                  <!-- Übersetzungen nur noch über das Zeilenmenü. -->
                  <RowMenu
                    v-help="'components.rowActions'"
                    :label="t('components.menuLabel', { name: c.Name })"
                    :items="menuItems(c)"
                    @select="key => onMenuSelect(c, key)"
                  />
                </td>
              </tr>
            </tbody>
          </table>
          <EmptyState v-else-if="searching" :title="t('components.noResults')" :text="t('components.noResultsText')" />
          <EmptyState v-else-if="archivedView" :title="t('components.emptyArchived')" :text="t('components.emptyArchivedText')" />
          <EmptyState v-else :title="t('components.empty')" />
        </div>
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">

        <!-- Pflichtfeld Name prüft saveItem selbst (Meldung am Feld). Ohne data-self-validates
             fängt der Submit-Wächter (utils/formValidation.js) den Submit ab und zeigt nur den
             englischen Browser-Hinweis statt der Feldmeldung. -->
        <form ref="formEl" novalidate data-self-validates class="edit-form" @submit.prevent="saveItem">
          <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
          <FormLanguageSwitch :trans="trans" />
          <fieldset class="form-lock" :disabled="formReadOnly">

          <FormField v-slot="f" :label="t('components.fieldName')" required help="components.name" :hint="t('components.nameCounter', { n: shownNameCount, max: NAME_MAX })" :error="onDefaultLang ? nameFieldError : null">
            <input
              :id="f.id"
              v-model="titleModel"
              :required="f.required"
              :aria-invalid="f.invalid"
              :aria-describedby="f.describedby"
              :placeholder="t('components.namePlaceholder')"
              :maxlength="NAME_MAX"
              @input="nameError = ''"
            >
          </FormField>

          <FormField v-slot="f" :label="t('components.fieldDescription')" help="components.description" :hint="t('components.descriptionCounter', { n: shownDescriptionCount, max: DESCRIPTION_MAX })" :error="onDefaultLang ? descriptionFieldError : null">
            <textarea :id="f.id" v-model="descriptionModel" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('components.descriptionPlaceholder')" :maxlength="DESCRIPTION_MAX" @input="descriptionError = ''"></textarea>
          </FormField>

          <FormLanguageDone :trans="trans" />

          <!-- Kennung (Slug). Beim Anlegen optional (leer = aus dem Namen), danach schreibgeschützt. -->
          <FormField v-slot="f" :label="t('components.fieldSlug')" help="components.slug" :hint="t(currentEditId ? 'components.slugHintEdit' : 'components.slugHintNew')" :error="slugFieldError">
            <input
              :id="f.id"
              v-model="formData.Slug"
              class="input-mono"
              :readonly="!!currentEditId"
              :aria-invalid="f.invalid"
              :aria-describedby="f.describedby"
              :placeholder="t('components.slugPlaceholder')"
              :maxlength="currentEditId ? undefined : SLUG_MAX"
              autocomplete="off"
              spellcheck="false"
              @input="slugError = ''"
            >
          </FormField>

          <div class="form-grid" :class="{ 'form-grid--single': !canSelectTags }">
            <FormField v-slot="f" :label="t('components.fieldGroup')" help="components.group">
              <select :id="f.id" v-model="formData.ParentID">
                <option v-for="o in parentOptions" :key="o.value ?? 'none'" :value="o.value">{{ o.label }}</option>
              </select>
            </FormField>

            <div v-if="canSelectTags" v-help="'components.tags'" class="form-field form-field--manual" role="group" aria-labelledby="components-tags-label">
              <div class="form-field__labelrow">
                <span id="components-tags-label" class="form-field__label">{{ t('components.fieldTags') }}</span>
                <span class="form-field__optional">{{ t('patterns.optional') }}</span>
              </div>
              <TagChips :tags="availableTags" :selected-ids="formData.TagIDs" @toggle="toggleTag" />
            </div>
          </div>

          <!-- unsichtbare Komponenten (bei Gruppen samt Mitgliedern) fehlen auf der
               öffentlichen Statusseite, bleiben hier aber sichtbar und per Webhook setzbar. -->
          <div v-help="'components.visible'" class="form-field form-field--manual" role="group" :aria-label="t('components.fieldVisible')">
            <OnyxSwitch v-model="formData.Visible" :label="t('components.fieldVisible')" />
            <p class="text-muted">{{ t(formData.Visible ? 'components.visibleOn' : 'components.visibleOff') }}</p>
          </div>

          <!-- Gruppen haben keinen setzbaren Status. -->
          <p v-if="formData.IsGroup" class="form-group text-muted group-status-hint">{{ t('components.groupStatusHint') }}</p>
          <fieldset v-else v-help="'components.status'" class="form-field form-field--set" role="group">
            <legend class="form-field__labelrow">
              <span class="form-field__label">{{ t(currentEditId ? 'components.fieldStatus' : 'components.fieldInitialStatus') }}</span>
              <span class="form-field__required" aria-hidden="true">*</span>
            </legend>
            <StatusTiles v-model="formData.Status" />
          </fieldset>

          </fieldset>
          <div class="form-actions">
            <OnyxButton v-help="'components.cancel'" :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton v-if="!formReadOnly" v-help="'components.save'" :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>

  </div>
</template>
