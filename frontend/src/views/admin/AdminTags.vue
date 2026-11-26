<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useFormPanel } from '../../composables/useFormPanel'
import AppIcon from '../../components/AppIcon.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import FormPanel from '../../components/FormPanel.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import RowMenu from '../../components/RowMenu.vue'
import EmptyState from '../../components/EmptyState.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import { formatRelative } from '../../utils/relativeTime'
import { confirmDialog } from '../../composables/useConfirm'
import type { StatusPage, Tag } from '../../types/api'

/** Edit form; status_page_id is the select's value ('' = global tag). */
interface TagForm {
  name?: string
  status_page_id?: number | string
}

const { apiFetch } = useApi()
const { isGlobalAdmin } = useAuth()
const { t, locale } = useI18n()

const tags = ref<Tag[]>([])
const pages = ref<StatusPage[]>([])
const scopeFilter = ref('all') // 'all' | 'global' | <status_page_id>
const query = ref('')
const isEditing = ref(false)
const currentEditItem = ref<'new' | number | null>(null)
const editName = ref('')
const formData = ref<TagForm>({})
const nameError = ref('')
const alertMsg = ref('')
const alertType = ref('warning')
// Message with a type (success dismisses itself); clearAlert removes the one of the previous step.
const clearAlert = () => { alertMsg.value = '' }
const showAlert = (msg: string, type = 'warning') => {
  alertMsg.value = msg
  alertType.value = type
}
const isSaving = ref(false)
const deletingId = ref<number | null>(null)
const { formEl, focusForm, focusInvalid } = useFormPanel()

// Direction C: title with the name instead of the ID.
const formTitle = computed(() => currentEditItem.value === 'new'
  ? t('tags.createTitle')
  : t('tags.editTitle', { name: editName.value }))

const breadcrumbs = usePageCrumbs(() => t('tags.title'))
const subtitle = computed(() => t('tags.subtitle', {
  tags: t('tags.countTags', { n: tags.value.length }, tags.value.length),
  global: tags.value.filter(tag => !tag.StatusPageID).length,
}))

const columns = computed(() => [
  { key: 'Name', label: t('tags.tableName') },
  { key: 'scope', label: t('tags.tableScope') },
  { key: 'UpdatedAt', label: t('tags.tableChanged') },
])

const loadData = async () => {
  try {
    const [tgs, p] = await Promise.all([
      apiFetch<Tag[] | null>('/api/tags'),
      apiFetch<StatusPage[] | null>('/api/pages'),
    ])
    tags.value = tgs || []
    pages.value = p || []
  } catch (err) {
    showAlert(t('common.loadError') + (err as Error).message)
  }
}

onMounted(loadData)

const pageName = (id: number | string | null | undefined) => pages.value.find(p => p.ID === id)?.Name || t('tags.pageFallback', { id })

const inScope = (tag: Tag) => {
  if (scopeFilter.value === 'all') return true
  if (scopeFilter.value === 'global') return !tag.StatusPageID
  return tag.StatusPageID === Number.parseInt(scopeFilter.value)
}
const searching = computed(() => query.value.trim() !== '')
const visibleTags = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return tags.value.filter(tag => inScope(tag) && tag.Name.toLowerCase().includes(needle))
})
const filtering = computed(() => searching.value || scopeFilter.value !== 'all')

const openEdit = (item: Tag | 'new') => {
  if (item === 'new') {
    formData.value = { name: '', status_page_id: '' }
  } else {
    formData.value = { name: item.Name, status_page_id: item.StatusPageID || '' }
  }
  currentEditItem.value = item === 'new' ? 'new' : item.ID
  editName.value = item === 'new' ? '' : item.Name
  nameError.value = ''
  isEditing.value = true
  focusForm()
}

const cancelEdit = () => {
  isEditing.value = false
  currentEditItem.value = null
  nameError.value = ''
  formData.value = {}
}

const saveItem = async () => {
  // Required field name: error at the field instead of a server error, focus on the field.
  if (!(formData.value.name || '').trim()) {
    nameError.value = t('tags.nameRequired')
    await focusInvalid()
    return
  }
  const isNew = currentEditItem.value === 'new'
  clearAlert()
  isSaving.value = true
  try {
    if (isNew) {
      const payload: { name?: string, status_page_id?: number } = { name: formData.value.name }
      if (formData.value.status_page_id) payload.status_page_id = parseInt(String(formData.value.status_page_id))
      await apiFetch('/api/tags', { method: 'POST', body: JSON.stringify(payload) })
    } else {
      // Rename: the scope (global vs. status page) is deliberately not changeable,
      // see UpdateTagHandler in backend/handlers/tag.go.
      await apiFetch(`/api/tags/${currentEditItem.value}`, {
        method: 'PUT',
        body: JSON.stringify({ name: formData.value.name })
      })
    }
    showAlert(t(isNew ? 'adminFeedback.tagCreated' : 'adminFeedback.tagRenamed'), 'success')
    cancelEdit()
    await loadData()
  } catch (err) {
    showAlert(t('common.saveError') + (err as Error).message)
  } finally {
    isSaving.value = false
  }
}

const menuItems = (tag: Tag) => [
  { key: 'edit', label: t('tags.renameAction'), icon: 'pencil' },
  { key: 'delete', label: deletingId.value === tag.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === tag.ID },
]
const onMenuSelect = (tag: Tag, key: string) => {
  if (key === 'edit') openEdit(tag)
  else deleteItem(tag)
}

const deleteItem = async (tag: Tag) => {
  // Open question: whether to show the number of affected resources/users
  // before deleting. Pragmatic decision (see the PR description): a simple
  // confirmation without an exact count, like the existing confirmation
  // pattern in AdminRoles.vue / AdminStatusPages.vue - assigning tags (and
  // hence a meaningful count) only came later anyway.
  if (!(await confirmDialog({ message: t('tags.confirmDelete', { name: tag.Name }) }))) return
  clearAlert()
  deletingId.value = tag.ID
  try {
    await apiFetch(`/api/tags/${tag.ID}`, { method: 'DELETE' })
    showAlert(t('adminFeedback.tagDeleted'), 'success')
    await loadData()
  } catch (err) {
    showAlert(t('common.deleteError') + (err as Error).message)
  } finally {
    deletingId.value = null
  }
}
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} mb-16`">
      {{ alertMsg }} <button class="alert-close" @click="alertMsg=''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('tags.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="isGlobalAdmin" #actions>
        <OnyxButton :label="t('tags.newButton')" @click="openEdit('new')" />
      </template>
    </PageHeader>

    <!-- Direction C: table card on the left, form as a side card on the right. -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard class="split__main" :title="t('tags.cardTitle')">
        <template #tools>
          <select v-model="scopeFilter" class="comp-filter" :aria-label="t('tags.filterLabel')">
            <option value="all">{{ t('tags.filterAll') }}</option>
            <option value="global">{{ t('tags.filterGlobalOnly') }}</option>
            <option v-for="p in pages" :key="p.ID" :value="String(p.ID)">{{ t('tags.filterPagePrefix', { name: p.Name }) }}</option>
          </select>
          <SearchBox v-model="query" :label="t('tags.searchLabel')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('tags.searchResult', { n: visibleTags.length }, visibleTags.length) }}</p>

        <ResponsiveTable v-if="visibleTags.length" data-table :columns="columns" :items="visibleTags" row-key="ID">
          <template #cell-Name="{ item }"><strong>{{ item.Name }}</strong></template>
          <template #cell-scope="{ item }">
            <span v-if="!item.StatusPageID" class="status-badge badge-info"><AppIcon name="globe" :size="12" /> {{ t('tags.scopeGlobalBadge') }}</span>
            <span v-else class="tag-pill">{{ pageName(item.StatusPageID) }}</span>
          </template>
          <template #cell-UpdatedAt="{ item }">
            <RelativeTime :value="item.UpdatedAt" />
          </template>
          <template v-if="isGlobalAdmin" #actions="{ item }">
            <RowMenu :label="t('tags.menuLabel', { name: item.Name })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="filtering" :title="t('tags.noResults')" :text="t('tags.noResultsText')" icon="tag" />
        <EmptyState v-else :title="t('tags.empty')" icon="tag" />
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <FormField v-slot="f" :label="t('tags.fieldName')" required field-id="tag-name" :error="nameError || null">
            <input
              :id="f.id"
              v-model="formData.name"
              :required="f.required"
              :aria-invalid="f.invalid"
              :aria-describedby="f.describedby"
              :placeholder="t('tags.namePlaceholder')"
              @input="nameError = ''"
            />
          </FormField>
          <FormField v-if="currentEditItem === 'new'" v-slot="f" :label="t('tags.fieldScope')" field-id="tag-scope" :hint="t('tags.scopeHint')">
            <select :id="f.id" v-model="formData.status_page_id" :aria-describedby="f.describedby">
              <option value="">{{ t('tags.scopeGlobalOption') }}</option>
              <option v-for="p in pages" :key="p.ID" :value="p.ID">{{ p.Name }}</option>
            </select>
          </FormField>
          <div class="form-group" v-else>
            <span class="adm-subhead">{{ t('tags.fieldScope') }}</span>
            <div>
              <span v-if="!formData.status_page_id" class="status-badge badge-info"><AppIcon name="globe" :size="12" /> {{ t('tags.scopeGlobalBadge') }}</span>
              <span v-else class="tag-pill">{{ pageName(formData.status_page_id) }}</span>
              <small class="adm-hint">{{ t('tags.scopeImmutableHint') }}</small>
            </div>
          </div>
          <div class="form-actions">
            <OnyxButton :label="t('common.cancel')" mode="outline" @click="cancelEdit" type="button" :disabled="isSaving" />
            <OnyxButton :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>
  </div>
</template>
