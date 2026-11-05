<script setup>
import { ref, computed, onMounted } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useSaveAction, useDeleteAction } from '../../composables/useAdminCrudActions'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useFormPanel } from '../../composables/useFormPanel'
import AppIcon from '../../components/AppIcon.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import FormPanel from '../../components/FormPanel.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import RowMenu from '../../components/RowMenu.vue'
import { formatRelative } from '../../utils/relativeTime'
import PermissionMatrix from '../../components/PermissionMatrix.vue'
import PermissionSummary from '../../components/PermissionSummary.vue'
import { ADMIN_DASHBOARD, normalizePermissions } from '../../utils/permissions'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import EmptyState from '../../components/EmptyState.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'

const { apiFetch } = useApi()
// Rollen sind eine globale Ressource (roles:*): nur Global Admins, daher kein Rechtecheck je Aktion.
const { isGlobalAdmin } = useAuth()
const { t, locale } = useI18n()

const items = ref([])
const isEditing = ref(false)
const currentEditItem = ref(null)
const formData = ref({})
const alertMsg = ref('')
const isSaving = ref(false)
const deletingId = ref(null)
const query = ref('')
const editName = ref('')
const nameError = ref('')
const { formEl, focusForm, focusInvalid } = useFormPanel()

// Richtung C: Titel mit Namen statt ID, Seitenkopf mit Zählung.
const formTitle = computed(() => currentEditItem.value === 'new'
  ? t('roles.createTitle')
  : t('roles.editTitle', { name: editName.value }))
const breadcrumbs = usePageCrumbs(() => t('roles.title'))
const subtitle = computed(() => t('roles.subtitle', { n: items.value.length }, items.value.length))
const searching = computed(() => query.value.trim() !== '')
const rows = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return items.value.filter(r => r.Name.toLowerCase().includes(needle))
})
// Die Matrix leitet ihre Zeilen aus den Tabs der Navigation ab; nur
// das Sonderrecht ohne Aktionen kommt als eigener Abschnitt „Zugriff“ dazu.
// seitenbezogene Berechtigung fürs „Dashboard“-Menüelement im
// öffentlichen Header (Statusseite) – bewusst getrennt von den Tab-Rechten, da
// sie nur den Zugang zum CMS-Bereich steuert.
const standalonePermissions = [ADMIN_DASHBOARD]

const columns = computed(() => [
  { key: 'Name', label: t('roles.tableName') },
  { key: 'Permissions', label: t('roles.tablePermissions') },
  { key: 'UpdatedAt', label: t('roles.tableChanged') },
])

const loadData = async () => {
  try {
    items.value = await apiFetch('/api/roles')
  } catch (err) {
    alertMsg.value = t('common.loadError') + err.message
  }
}

onMounted(loadData)

const openEdit = (item) => {
  if (item === 'new') {
    formData.value = { permissions: [] }
  } else {
    formData.value = { ...item }
    // Alt-Rollen (`x:write`, fehlendes read) erscheinen korrekt als CRUD.
    formData.value.permissions = normalizePermissions(formData.value.Permissions)
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

const saveAction = useSaveAction({ apiFetch, isSaving, reload: loadData, onError: (err) => { alertMsg.value = t('common.saveError') + err.message } })
const deleteAction = useDeleteAction({ apiFetch, deletingId, reload: loadData, onError: (err) => { alertMsg.value = t('common.deleteError') + err.message } })

const saveItem = async () => {
  if (!(formData.value.Name || '').trim()) {
    nameError.value = t('roles.nameRequired')
    await focusInvalid()
    return
  }
  const isNew = currentEditItem.value === 'new'
  const endpoint = `/api/roles${isNew ? '' : '/' + currentEditItem.value}`

  const payload = { ...formData.value }
  if (Array.isArray(payload.permissions)) {
    payload.Permissions = payload.permissions.join(',')
    delete payload.permissions
  }

  await saveAction(endpoint, isNew ? 'POST' : 'PUT', payload, cancelEdit)
}

const deleteItem = (role) => deleteAction(`/api/roles/${role.ID}`, role.ID, t('roles.confirmDelete', { name: role.Name }))

const menuItems = (role) => [
  { key: 'edit', label: t('common.edit'), icon: 'pencil' },
  { key: 'delete', label: deletingId.value === role.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === role.ID },
]
const onMenuSelect = (role, key) => {
  if (key === 'edit') openEdit(role)
  else deleteItem(role)
}
</script>

<template>
  <div>
    <div v-if="alertMsg" class="alert alert-warning mb-16">
      {{ alertMsg }} <button class="alert-close" @click="alertMsg=''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('roles.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="isGlobalAdmin" #actions>
        <OnyxButton :label="t('roles.newButton')" @click="openEdit('new')" />
      </template>
    </PageHeader>

    <!-- Richtung C: Tabellenkarte links, Formular als Seitenkarte rechts. -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard class="split__main" :title="t('roles.cardTitle')">
        <template #tools>
          <SearchBox v-model="query" :label="t('roles.searchLabel')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('roles.searchResult', { n: rows.length }, rows.length) }}</p>

        <ResponsiveTable v-if="rows.length" data-table :columns="columns" :items="rows" row-key="ID">
          <template #cell-Name="{ item }"><strong>{{ item.Name }}</strong></template>
          <template #cell-Permissions="{ item }">
            <PermissionSummary :permissions="item.Permissions" />
          </template>
          <template #cell-UpdatedAt="{ item }">
            <RelativeTime :value="item.UpdatedAt" />
          </template>
          <template v-if="isGlobalAdmin" #actions="{ item }">
            <RowMenu :label="t('roles.menuLabel', { name: item.Name })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="searching" :title="t('roles.noResults')" :text="t('roles.noResultsText')" icon="shield" />
        <EmptyState v-else :title="t('roles.empty')" icon="shield" />
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <FormField v-slot="f" :label="t('roles.fieldName')" required field-id="role-name" :error="nameError || null">
            <input
              :id="f.id"
              v-model="formData.Name"
              :required="f.required"
              :aria-invalid="f.invalid"
              :aria-describedby="f.describedby"
              @input="nameError = ''"
            />
          </FormField>
          <PermissionMatrix v-model="formData.permissions" :standalone="standalonePermissions" />
          <div class="form-actions">
            <OnyxButton :label="t('common.cancel')" mode="outline" @click="cancelEdit" type="button" :disabled="isSaving" />
            <OnyxButton :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>
  </div>
</template>
