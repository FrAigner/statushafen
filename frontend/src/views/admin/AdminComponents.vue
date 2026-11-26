<script setup lang="ts">
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
import type { ComponentTreeNode, FlatComponent } from '../../utils/componentTree'
import type { ApiError, Component } from '../../types/api'

/** A component as the tree endpoint sends it. */
type ComponentNode = Component & ComponentTreeNode
/** A row of the flattened tree (with its depth). */
type ComponentRow = FlatComponent<ComponentNode>

/** Edit form: the component fields plus the selected tag IDs. */
interface ComponentForm extends Partial<Component> {
  TagIDs?: number[]
}

const { apiFetch } = useApi()
// Permissions of this tab per action. Without update an existing entry opens
// read-only (formReadOnly); without delete the row menu entry is gone.
const { canCreate, canUpdate, canDelete } = useResourcePerms('components')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()
// Help texts (help.components.*) for every field and button via v-help
// (appears after hovering, can be switched off per user).

const items          = ref<ComponentRow[]>([])
const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, activeTenantId)
const isEditing      = ref(false)
const currentEditId  = ref<number | null>(null)
const editName       = ref('')
const formData       = ref<ComponentForm>({})

// Language switcher above name and description (further languages of the
// page). The description is optional; a translation only needs the name.
// Translations follow the tab's "update" permission.
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
const formEl         = ref<HTMLFormElement | null>(null)
const nameError      = ref('')
const descriptionError = ref('')
const slugError      = ref('')
const forbidden      = ref(false)
const alertMsg       = ref('')
const alertType      = ref('warning')
const isSaving       = ref(false)
const deletingId     = ref<number | null>(null)
const query          = ref('')
const collapsed      = ref(new Set<number>())

// Direction C: the title names the name, not the ID.
const formTitle = computed(() => {
  if (currentEditId.value) return t(formData.value.IsGroup ? 'components.editGroupTitle' : 'components.editTitle', { name: editName.value })
  return t(formData.value.IsGroup ? 'components.createGroupTitle' : 'components.createTitle')
})

/* ── Field lengths ── */
// Limits as in the backend (handlers/component.go); counted in characters
// (code points), not in UTF-16 units, so that umlauts and emojis count as one
// each, as there.
const NAME_MAX = 60
const DESCRIPTION_MAX = 240
// Slug, format and length as in the backend (models/component_slug.go).
const SLUG_MAX = 60
const SLUG_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/
const charCount = (value: string | null | undefined) => [...(value || '')].length
const nameCount = computed(() => charCount(formData.value.Name))
const descriptionCount = computed(() => charCount(formData.value.Description))
// Shown is the counter of the field in the active language tab.
const shownNameCount = computed(() => charCount(titleModel.value))
const shownDescriptionCount = computed(() => charCount(descriptionModel.value))
// Existing texts can be longer than allowed (saved before the change): they
// are not truncated, but the field reports the excess right away so that the
// next save does not come as a surprise.
const nameFieldError = computed(() => nameError.value || (nameCount.value > NAME_MAX ? t('components.nameTooLong', { max: NAME_MAX }) : null))
// The slug can only be set when creating (read-only afterwards).
const slugValue = computed(() => (formData.value.Slug || '').trim())
const slugFieldError = computed(() => slugError.value || (currentEditId.value === null && slugValue.value && (slugValue.value.length > SLUG_MAX || !SLUG_RE.test(slugValue.value)) ? t('components.slugInvalid', { max: SLUG_MAX }) : null))
const descriptionFieldError = computed(() => descriptionError.value || (descriptionCount.value > DESCRIPTION_MAX ? t('components.descriptionTooLong', { max: DESCRIPTION_MAX }) : null))

/* ── Page header: breadcrumbs and subtitle ── */
const breadcrumbs = usePageCrumbs(() => t('components.title'))
// Active/archived. Archived entries (ArchivedAt) only appear in the
// "Archived" segment and do not count in the subtitle.
const isArchived = (c: ComponentRow) => !!c.ArchivedAt
const activeItems = computed(() => items.value.filter(c => !isArchived(c)))
const archivedItems = computed(() => items.value.filter(isArchived))
const view = ref('active')
const viewOptions = computed(() => [
  { value: 'active', label: t('components.viewActive', { n: activeItems.value.length }) },
  { value: 'archived', label: t('components.viewArchived', { n: archivedItems.value.length }) },
])
const archivedView = computed(() => view.value === 'archived')
// Recompute the depth: an individually archived member of an active group
// would otherwise sit indented without its group in the list.
const withShownDepth = (list: ComponentRow[]) => {
  const depthOf = new Map<number | null | undefined, number>()
  return list.map(c => {
    const depth = depthOf.has(c.ParentID) ? depthOf.get(c.ParentID)! + 1 : 0
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

/* ── List: search and collapsible groups ── */
const searchText = (c: ComponentRow) => `${c.Name} ${c.Slug || ''} ${c.Description || ''} ${(c.Tags || []).map(tag => tag.Name).join(' ')}`
const rows = computed(() => visibleComponents(shownItems.value, { query: query.value, collapsed: collapsed.value, textOf: searchText }))
const searching = computed(() => query.value.trim() !== '')

const toggleGroup = (id: number) => {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
}

/* ── Form: group (parent component) ── */
// Selectable as a group are groups, except the component itself and its
// descendants (the backend rejects cycles). If the current parent component
// is not a group, it stays selectable anyway, otherwise it would be lost on
// save.
const parentOptions = computed(() => {
  const excluded = new Set(currentEditId.value ? [currentEditId.value, ...descendantIds(items.value, currentEditId.value)] : [])
  const current = formData.value.ParentID
  const candidates = items.value.filter(c => !excluded.has(c.ID) && (c.IsGroup || c.ID === current))
  return [{ label: t('components.groupNone'), value: null }, ...componentOptions(candidates, [current as number]).map(o => ({ ...o, value: Number(o.value) }))]
})

/* ── Data loading ── */
const loadData = async () => {
  forbidden.value = false
  try {
    // The API returns a tree. Flat in tree order with depth, so that
    // subcomponents are listed and editable too.
    items.value = flattenComponentTree(await apiFetch<ComponentNode[]>(`/api/pages/${activeTenantId.value}/components`))
  } catch (err) {
    // 403 -> permission notice instead of a load error alert.
    if (isForbidden(err)) forbidden.value = true
    else showAlert(t('common.loadError') + (err as Error).message, 'error')
  }
  // Tags only for global admins (see useAvailableTags).
  await loadAvailableTags()
  // further languages of the page for the switcher.
  await trans.load(activeTenantId.value!)
}

const toggleTag = useTagToggle(formData)

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

// Open the form: focus on the name field; below 860 px the form sits below
// the list and is scrolled into view.
const focusForm = async () => {
  await nextTick()
  formEl.value?.closest('aside')?.scrollIntoView?.({ block: 'nearest' })
  formEl.value?.querySelector('input')?.focus({ preventScroll: true })
}

// isGroup creates a group (no status; it is computed from the members).
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

const openEdit = (item: ComponentRow) => {
  // Visible is only missing with old API versions: then it counts as visible.
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

// Length errors of the server (race with another client, older frontend
// versions) end up at the field instead of as a general message.
const onSaveError = (err: ApiError) => {
  if (err.data?.code === 'slug_invalid') slugError.value = t('components.slugInvalid', { max: SLUG_MAX })
  else if (err.data?.code === 'slug_taken') slugError.value = t('components.slugTaken')
  else if (err.data?.code === 'name_too_long') nameError.value = t('components.nameTooLong', { max: NAME_MAX })
  else if (err.data?.code === 'description_too_long') descriptionError.value = t('components.descriptionTooLong', { max: DESCRIPTION_MAX })
  else showAlert(t('common.saveError') + err.message, 'error')
}
const saveAction = useSaveAction({ apiFetch, isSaving, reload: loadData, onStart: clearAlert, onError: onSaveError })
// 409 group_not_empty (the group has been filled in the meantime) with an
// understandable, translated message instead of the English server message.
const deleteAction = useDeleteAction({
  apiFetch, deletingId, reload: loadData, onStart: clearAlert,
  onDeleted: () => showAlert(t('adminFeedback.deleted'), 'success'),
  onError: (err) => showAlert(err.data?.code === 'group_not_empty'
    ? t('components.groupNotEmpty')
    : t('common.deleteError') + err.message, 'error'),
})

const saveItem = async () => {
  // Required field name: error at the field instead of a server error, focus on the field.
  if (!(formData.value.Name || '').trim()) nameError.value = t('components.nameRequired')
  if (nameFieldError.value || descriptionFieldError.value || slugFieldError.value) {
    activeLang.value = '' // the checks belong to the default language
    await nextTick()
    formEl.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
    return
  }
  if (!trans.checkComplete(showAlert)) return
  const isNew = currentEditId.value === null
  const url   = isNew
    ? `/api/pages/${activeTenantId.value}/components`
    : `/api/pages/${activeTenantId.value}/components/${currentEditId.value}`
  // Payload built explicitly instead of serializing formData directly: the
  // backend request DTO expects "tag_ids" (snake_case); formData.TagIDs would
  // arrive as the JSON key "TagIDs", which does NOT match "tag_ids"
  // case-insensitively (unlike e.g. "Name"/"name").
  const payload: Record<string, unknown> = {
    name: formData.value.Name,
    description: formData.value.Description,
    tag_ids: formData.value.TagIDs || [],
    // 0 = top level (when editing this removes the assignment).
    parent_id: formData.value.ParentID || 0,
    // "Show on the status page".
    visible: formData.value.Visible !== false,
  }
  // For groups the status is the value computed from the children.
  // Sending it back would have turned the group into a normal component.
  if (!formData.value.IsGroup) payload.status = formData.value.Status
  // New group without a status, but with is_group.
  else if (isNew) payload.is_group = true
  // Only send the slug when creating; empty = automatic.
  if (isNew && slugValue.value) payload.slug = slugValue.value
  await saveAction(url, isNew ? 'POST' : 'PUT', payload, async (saved: { ID?: number } | null) => {
    // Remember the new ID before translations follow: if one fails, a second attempt does not create a second component.
    if (isNew && saved?.ID) currentEditId.value = saved.ID
    if (showSwitcher.value) await trans.saveAll(currentEditId.value!)
    const createdKey = formData.value.IsGroup ? 'components.groupCreated' : 'components.created'
    showAlert(isNew ? t(createdKey) : t('common.changesSaved'), 'success')
    cancelEdit()
  })
}

const deleteItem = (id: number, name: string) =>
  deleteAction(`/api/pages/${activeTenantId.value}/components/${id}`, id, t('components.confirmDelete', { name }))

// Row menu: entries per row; "Translations" is a link.
// A group with members cannot be deleted (backend: 409).
const isNonEmptyGroup = (c: ComponentRow) => !!c.IsGroup && items.value.some(x => x.ParentID === c.ID)
const deleteLabel = (c: ComponentRow) => {
  if (deletingId.value === c.ID) return t('common.deleting')
  return isNonEmptyGroup(c) ? t('components.deleteGroupBlocked') : t('common.delete')
}
const menuItems = (c: ComponentRow) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  // Only with a second content language (switched off: the page lists its default only).
  ...(trans.multilingual.value ? [{ key: 'translations', label: t('translations.manageLink'), icon: 'globe', to: `/admin/components/${c.ID}/translations` }] : []),
  ...(canUpdate.value ? [isArchived(c)
    ? { key: 'restore', label: t('components.restore'), icon: 'arrow-left' }
    : { key: 'archive', label: t('components.archive'), icon: 'folder' }] : []),
  ...(canDelete.value ? [{ key: 'delete', label: deleteLabel(c), icon: 'trash', danger: true, disabled: deletingId.value === c.ID || isNonEmptyGroup(c) }] : []),
]
const onMenuSelect = (c: ComponentRow, key: string) => {
  if (key === 'edit') openEdit(c)
  else if (key === 'archive' || key === 'restore') setArchived(c, key)
  else deleteItem(c.ID, c.Name) // "Translations" is a link and reports nothing
}

// Archive/restore via their own routes; the list reloads, because a group
// takes its members along.
const setArchived = async (comp: ComponentRow, action: 'archive' | 'restore') => {
  clearAlert()
  try {
    await apiFetch(`/api/pages/${activeTenantId.value}/components/${comp.ID}/${action}`, { method: 'POST' })
    showAlert(t(action === 'archive' ? 'components.archived' : 'components.restored'), 'success')
    await loadData()
  } catch (e) {
    const err = e as ApiError
    showAlert(err.data?.code === 'parent_archived'
      ? t('components.parentArchived')
      : t(action === 'archive' ? 'components.archiveError' : 'components.restoreError') + err.message, 'error')
  }
}

const quickStatus = async (comp: ComponentRow, newStatus: string) => {
  try {
    await apiFetch(`/api/pages/${activeTenantId.value}/components/${comp.ID}`, {
      method: 'PUT',
      // only send the status; the backend leaves missing fields unchanged.
      // The whole row would now also have sent Children and depth.
      body: JSON.stringify({ status: newStatus })
    })
    // Feedback as with the form, instead of leaving an older message
    // (e.g. "Component created.") in place.
    showAlert(t('common.changesSaved'), 'success')
    loadData()
  } catch (err) {
    showAlert(t('components.quickStatusError') + (err as Error).message, 'error')
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

    <!-- Stage 4c: list and form sit side by side
         (stacked below 860 px); the form no longer replaces the list. -->
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
              <!-- Groups (IsGroup) are collapsible group rows. -->
              <tr v-for="c in rows" :key="c.ID" :class="{ 'data-table__group': c.IsGroup }">
                <component :is="c.IsGroup ? 'th' : 'td'" :scope="c.IsGroup ? 'row' : undefined" :data-label="t('components.tableName')">
                  <!-- Subcomponents indented by their depth in the tree. -->
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
                  <!-- Groups only display the computed status. -->
                  <span v-if="c.IsGroup" class="sr-only">{{ t('components.groupLabel') }}</span>
                </td>
                <td :data-label="t('components.tableChanged')" class="cell-changed">
                  <RelativeTime :value="c.UpdatedAt" />
                </td>
                <td class="actions-col">
                  <!-- Translations only via the row menu now. -->
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

        <!-- saveItem checks the required name field itself (message at the field). Without
             data-self-validates the submit guard (utils/formValidation.ts) intercepts the submit
             and only shows the English browser hint instead of the field message. -->
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

          <!-- Slug. Optional when creating (empty = from the name), read-only afterwards. -->
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
              <TagChips :tags="availableTags" :selected-ids="formData.TagIDs!" @toggle="toggleTag" />
            </div>
          </div>

          <!-- invisible components (for groups including their members) are missing from the
               public status page, but stay visible here and settable via webhook. -->
          <div v-help="'components.visible'" class="form-field form-field--manual" role="group" :aria-label="t('components.fieldVisible')">
            <OnyxSwitch v-model="formData.Visible" :label="t('components.fieldVisible')" />
            <p class="text-muted">{{ t(formData.Visible ? 'components.visibleOn' : 'components.visibleOff') }}</p>
          </div>

          <!-- Groups have no settable status. -->
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
