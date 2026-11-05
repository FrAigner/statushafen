<script setup>
import { ref, computed, onMounted } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useTagToggle } from '../../composables/useTagSelection'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useFormPanel } from '../../composables/useFormPanel'
import { useSaveAction, useDeleteAction } from '../../composables/useAdminCrudActions'
import AppIcon from '../../components/AppIcon.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormPanel from '../../components/FormPanel.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import RowMenu from '../../components/RowMenu.vue'
import { formatRelative } from '../../utils/relativeTime'
import FormField from '../../components/FormField.vue'
import EmptyState from '../../components/EmptyState.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import TagCheckboxList from '../../components/TagCheckboxList.vue'
import { pageRoleOptions, userAdminErrorText } from '../../utils/userAdmin'
import { MAX_PASSWORD_BYTES, MIN_PASSWORD_LENGTH, isPasswordTooLong, isPasswordTooShort, serverInputErrorText } from '../../utils/inputRules'

const { apiFetch } = useApi()
const { isGlobalAdmin, currentUser } = useAuth()
const { t, locale } = useI18n()

const items = ref([])
const roles = ref([])
const pages = ref([])
const availableTags = ref([])
const isEditing = ref(false)
const currentEditItem = ref(null)
const formData = ref({ PageRoles: [], TagIDs: [] })
const alertMsg = ref('')
const isSaving = ref(false)
const deletingId = ref(null)
const query = ref('')
const editName = ref('')
const errors = ref({ email: '', password: '' })
const { formEl, focusForm, focusInvalid } = useFormPanel()

const columns = computed(() => [
  { key: 'Email', label: t('users.tableEmail') },
  { key: 'access', label: t('users.tableAccess') },
  { key: 'tags', label: t('users.tableTags') },
  { key: 'UpdatedAt', label: t('users.tableChanged') },
])

// Richtung C: Titel mit Namen statt ID, Seitenkopf mit Zählung.
const formTitle = computed(() => currentEditItem.value === 'new'
  ? t('users.createTitle')
  : t('users.editTitle', { name: editName.value }))
const breadcrumbs = usePageCrumbs(() => t('users.title'))
const subtitle = computed(() => {
  const admins = items.value.filter(u => u.IsGlobalAdmin).length
  return t('users.subtitle', {
    users: t('users.countUsers', { n: items.value.length }, items.value.length),
    admins: t('users.countAdmins', { n: admins }, admins),
  })
})
const searching = computed(() => query.value.trim() !== '')
const rows = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return items.value.filter(u => `${u.Email} ${(u.Tags || []).map(tag => tag.Name).join(' ')}`.toLowerCase().includes(needle))
})

const loadData = async () => {
  try {
    // /api/tags ist global-admin-only - Laden hier bewusst
    // fehlertolerant, damit ein Nicht-Global-Admin (falls diese Seite jemals
    // ohne isGlobalAdmin-Gate aufgerufen würde) nicht am 403 scheitert, nur
    // ohne Tag-Auswahl weiterarbeitet.
    const [usr, rls, pgs] = await Promise.all([
      apiFetch('/api/users'),
      apiFetch('/api/roles'),
      apiFetch('/api/pages')
    ])
    items.value = usr || []
    roles.value = rls || []
    pages.value = pgs || []
    try {
      availableTags.value = (await apiFetch('/api/tags')) || []
    } catch {
      availableTags.value = []
    }
  } catch (err) {
    alertMsg.value = t('common.loadError') + err.message
  }
}

const toggleTag = useTagToggle(formData)

// Benutzer-Tags sind nur global gültig (das Backend lehnt
// seitengebundene mit 400 ab). Die seitengebundenen erschienen hier trotzdem,
// gleichnamige mehrfach und ohne Seitennamen nicht unterscheidbar.
const globalTags = computed(() => availableTags.value.filter(tag => !tag.StatusPageID))

// eigenes Konto und letzter Global Admin. Das Backend lehnt
// Löschen/Herabstufen mit 409 ab; die Oberfläche bietet es gar nicht erst an.
const globalAdminCount = computed(() => items.value.filter(u => u.IsGlobalAdmin).length)
const isSelf = (u) => currentUser.value?.id != null && u.ID === currentUser.value.id
const isLastGlobalAdmin = (u) => u.IsGlobalAdmin && globalAdminCount.value === 1
const deleteBlockedHint = (u) => {
  if (isSelf(u)) return t('users.selfRowHint')
  if (isLastGlobalAdmin(u)) return t('users.lastAdminRowHint')
  return ''
}
const adminLockHint = computed(() => {
  const edited = items.value.find(u => u.ID === currentEditItem.value)
  if (!edited?.IsGlobalAdmin) return ''
  if (isSelf(edited)) return t('users.selfDemoteHint')
  if (isLastGlobalAdmin(edited)) return t('users.lastAdminDemoteHint')
  return ''
})

onMounted(loadData)

const getRoleForPage = (pageId) => {
  const pr = formData.value.PageRoles.find(p => p.status_page_id === pageId)
  return pr ? pr.role_id : 0
}

const setRoleForPage = (pageId, roleIdStr) => {
  const roleId = Number.parseInt(roleIdStr) || 0
  const existing = formData.value.PageRoles.find(p => p.status_page_id === pageId)
  
  if (existing) {
    existing.role_id = roleId
  } else {
    formData.value.PageRoles.push({ status_page_id: pageId, role_id: roleId })
  }
}

const openEdit = (item) => {
  if (item === 'new') {
    const defaultPageRoles = pages.value
      .filter(p => p.DefaultRoleID)
      .map(p => ({
        status_page_id: p.ID,
        role_id: p.DefaultRoleID
      }))
      
    formData.value = {
      Email: '',
      Password: '',
      TagIDs: [],
      IsGlobalAdmin: false,
      PageRoles: defaultPageRoles
    }
  } else {
    formData.value = {
      Email: item.Email,
      Password: '',
      TagIDs: (item.Tags || []).map(t => t.ID),
      IsGlobalAdmin: item.IsGlobalAdmin || false,
      PageRoles: (item.UserStatusPageRoles || []).map(r => ({
        status_page_id: r.StatusPageID,
        role_id: r.RoleID
      }))
    }
  }
  currentEditItem.value = item === 'new' ? 'new' : item.ID
  editName.value = item === 'new' ? '' : item.Email
  errors.value = { email: '', password: '' }
  isEditing.value = true
  focusForm()
}

const cancelEdit = () => {
  isEditing.value = false
  currentEditItem.value = null
  errors.value = { email: '', password: '' }
  formData.value = { PageRoles: [] }
}

// zu kurzes oder zu langes Passwort (Backend: 400 password_too_short bzw.
// password_too_long) steht am Feld.
const onSaveError = async (err) => {
  const text = ['password_too_short', 'password_too_long'].includes(err.data?.code) ? serverInputErrorText(err, t) : null
  if (!text) {
    alertMsg.value = t('common.saveError') + userAdminErrorText(err, t)
    return
  }
  errors.value = { ...errors.value, password: text }
  await focusInvalid()
}
const saveAction = useSaveAction({ apiFetch, isSaving, reload: loadData, onError: onSaveError })
const deleteAction = useDeleteAction({ apiFetch, deletingId, reload: loadData, onError: (err) => { alertMsg.value = t('common.deleteError') + userAdminErrorText(err, t) } })

// Neu: Pflicht; beim Ändern leer = unverändert. Gesetzt gilt die Mindestlänge.
const passwordError = (isNew) => {
  const pw = formData.value.Password
  if (isNew && !pw) return t('users.passwordRequired')
  if (pw && isPasswordTooShort(pw)) return t('inputErrors.password_too_short', { min: MIN_PASSWORD_LENGTH })
  return pw && isPasswordTooLong(pw) ? t('inputErrors.password_too_long', { max: MAX_PASSWORD_BYTES }) : ''
}

const saveItem = async () => {
  const isNew = currentEditItem.value === 'new'
  // Pflichtfelder: Fehler am Feld statt eines Serverfehlers, Fokus aufs erste fehlerhafte Feld.
  errors.value = {
    email: (formData.value.Email || '').trim() ? '' : t('users.emailRequired'),
    password: passwordError(isNew),
  }
  if (errors.value.email || errors.value.password) {
    await focusInvalid()
    return
  }
  const endpoint = `/api/users${isNew ? '' : '/' + currentEditItem.value}`

  const payload = {
    email: formData.value.Email,
    password: formData.value.Password,
    tag_ids: formData.value.TagIDs,
    is_global_admin: formData.value.IsGlobalAdmin,
    page_roles: formData.value.PageRoles.filter(pr => pr.role_id > 0)
  }

  await saveAction(endpoint, isNew ? 'POST' : 'PUT', payload, cancelEdit)
}

// Die Löschfrage nennt die E-Mail, damit klar ist, wen sie trifft.
const deleteItem = (user) => deleteAction(`/api/users/${user.ID}`, user.ID, t('users.confirmDelete', { email: user.Email }))

// Eigenes Konto und letzter Global Admin bieten kein Löschen an.
const menuItems = (user) => [
  { key: 'edit', label: t('common.edit'), icon: 'pencil' },
  ...(deleteBlockedHint(user)
    ? []
    : [{ key: 'delete', label: deletingId.value === user.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === user.ID }]),
]
const onMenuSelect = (user, key) => {
  if (key === 'edit') openEdit(user)
  else deleteItem(user)
}

const summarizeRoles = (user) => {
  if (user.IsGlobalAdmin) return t('users.globalAdminSummary')
  if (!user.UserStatusPageRoles || user.UserStatusPageRoles.length === 0) return t('users.noRightsSummary')

  const count = user.UserStatusPageRoles.length
  return t('users.pagesCountSummary', { count }, count)
}
</script>

<template>
  <div>
    <div v-if="alertMsg" class="alert alert-warning mb-16">
      {{ alertMsg }} <button class="alert-close" @click="alertMsg=''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('users.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="isGlobalAdmin" #actions>
        <OnyxButton :label="t('users.newButton')" @click="openEdit('new')" />
      </template>
    </PageHeader>

    <!-- Richtung C: Tabellenkarte links, Formular als Seitenkarte rechts. -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard class="split__main" :title="t('users.cardTitle')">
        <template #tools>
          <SearchBox v-model="query" :label="t('users.searchLabel')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('users.searchResult', { n: rows.length }, rows.length) }}</p>

        <ResponsiveTable v-if="rows.length" data-table :columns="columns" :items="rows" row-key="ID">
          <template #cell-Email="{ item }">
            <strong>{{ item.Email }}</strong>
            <small v-if="isGlobalAdmin && deleteBlockedHint(item)" class="cell-sub delete-blocked-hint">{{ deleteBlockedHint(item) }}</small>
          </template>
          <template #cell-access="{ item }">
            <span class="tag-pill" :class="{ 'tag-pill--accent': item.IsGlobalAdmin }">{{ summarizeRoles(item) }}</span>
          </template>
          <template #cell-tags="{ item }">
            <span v-if="!(item.Tags || []).length" class="text-muted">—</span>
            <span v-else class="tag-pills"><span v-for="tag in item.Tags" :key="tag.ID" class="tag-pill">{{ tag.Name }}</span></span>
          </template>
          <template #cell-UpdatedAt="{ item }">
            <RelativeTime :value="item.UpdatedAt" />
          </template>
          <template v-if="isGlobalAdmin" #actions="{ item }">
            <RowMenu :label="t('users.menuLabel', { name: item.Email })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else-if="searching" :title="t('users.noResults')" :text="t('users.noResultsText')" icon="users" />
        <EmptyState v-else :title="t('users.empty')" icon="users" />
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <FormField v-slot="f" :label="t('users.fieldEmail')" required field-id="user-email" :error="errors.email || null">
            <input
              :id="f.id"
              v-model="formData.Email"
              type="email"
              :required="f.required"
              :aria-invalid="f.invalid"
              :aria-describedby="f.describedby"
              @input="errors.email = ''"
            />
          </FormField>

          <FormField
            v-slot="f"
            :label="t('users.fieldPassword')"
            :required="currentEditItem === 'new'"
            :hint="currentEditItem !== 'new' ? t('users.passwordHint') : null"
            :error="errors.password || null"
            field-id="user-password"
          >
            <input
              :id="f.id"
              v-model="formData.Password"
              type="password"
              :required="f.required"
              :aria-invalid="f.invalid"
              :aria-describedby="f.describedby"
              @input="errors.password = ''"
            />
          </FormField>

          <div class="checkbox-group">
            <label>
              <input type="checkbox" v-model="formData.IsGlobalAdmin" :disabled="!!adminLockHint" :aria-describedby="adminLockHint ? 'user-admin-lock-hint' : undefined" />
              <span>{{ t('users.globalAdminLabelPrefix') }} <strong>{{ t('users.globalAdminLabelBold') }}</strong> {{ t('users.globalAdminLabelSuffix') }}</span>
            </label>
            <p v-if="adminLockHint" id="user-admin-lock-hint" class="lock-hint">{{ adminLockHint }}</p>
          </div>

          <div v-if="!formData.IsGlobalAdmin" class="form-field form-field--manual" role="group" aria-labelledby="user-page-access-label">
            <div class="form-field__labelrow">
              <span id="user-page-access-label" class="form-field__label">{{ t('users.pageAccessHeading') }}</span>
            </div>
            <div class="page-roles-list">
              <div v-for="page in pages" :key="page.ID" class="page-role-row">
                <div class="page-name">{{ page.Name }}</div>
                <div class="page-select">
                  <select :value="getRoleForPage(page.ID)" :aria-label="t('users.roleForPageAria', { name: page.Name })" @change="e => setRoleForPage(page.ID, e.target.value)">
                    <option value="0">{{ t('users.noAccessOption') }}</option>
                    <option v-for="r in pageRoleOptions(roles, getRoleForPage(page.ID))" :key="r.ID" :value="r.ID">{{ r.Name }}</option>
                  </select>
                </div>
              </div>
              <EmptyState v-if="pages.length === 0" :title="t('users.noPagesHint')" />
            </div>
          </div>

          <div class="form-field form-field--manual" role="group" aria-labelledby="user-tags-label">
            <div class="form-field__labelrow">
              <span id="user-tags-label" class="form-field__label">{{ t('users.fieldTags') }}</span>
              <span class="form-field__optional">{{ t('patterns.optional') }}</span>
            </div>
            <TagCheckboxList :tags="globalTags" :selected-ids="formData.TagIDs" @toggle="toggleTag" />
          </div>

          <div class="form-actions">
            <OnyxButton :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>
  </div>
</template>

<style scoped>
.checkbox-group {
  margin-bottom: var(--sp-4);
  padding: var(--sp-3);
  background-color: var(--surface-2);
  border-radius: var(--r-md);
  border: 1px solid var(--border);
}
.checkbox-group label {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-2);
  cursor: pointer;
  margin: 0;
}
.checkbox-group input {
  width: auto;
  margin-top: 3px;
  cursor: pointer;
}
.page-roles-list {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
}
.page-role-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-2);
  padding: var(--sp-2) var(--sp-3);
  background-color: var(--surface-2);
  border-radius: var(--r-md);
  border: 1px solid var(--border);
}
.page-name {
  font-weight: 600;
  color: var(--text);
}
.lock-hint,
.delete-blocked-hint {
  color: var(--text-2);
  font-size: var(--fs-sm);
}
.lock-hint {
  margin: var(--sp-2) 0 0;
}
</style>
