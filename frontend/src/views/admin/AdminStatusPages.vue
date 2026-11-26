<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick, inject } from 'vue'
import { routeLocationKey, routerKey } from 'vue-router'
import { OnyxButton, OnyxModal } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useSaveAction, useDeleteAction } from '../../composables/useAdminCrudActions'
import AppIcon from '../../components/AppIcon.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import PageHeader from '../../components/PageHeader.vue'
import EmptyState from '../../components/EmptyState.vue'
import FormField from '../../components/FormField.vue'
import FormPanel from '../../components/FormPanel.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import RowMenu from '../../components/RowMenu.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import StatusPage from '../public/StatusPage.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { formatRelative } from '../../utils/relativeTime'
import { serverInputErrorText } from '../../utils/inputRules'
import { primaryBaseDomain } from '../../utils/appDomains'
// "Global Admin" is not a page role (GlobalOnly from /api/roles).
import { pageRoleOptions, userAdminErrorText } from '../../utils/userAdmin'
import { confirmDialog } from '../../composables/useConfirm'
import type { ApiError, Role, StatusPage as StatusPageModel, User, UserStatusPageRole } from '../../types/api'
import type { CustomDomainResponse, PageInvitation } from '../../types/admin'

/** Edit form of a status page; DefaultRoleID is the select's value. */
interface StatusPageForm extends Omit<Partial<StatusPageModel>, 'DefaultRoleID'> {
  DefaultRoleID?: number | string | null
}

/** A user's assignment to the page as GET /api/admin/pages/:id/users returns it. */
interface PageUserAssignment extends UserStatusPageRole {
  User?: User
}

const { apiFetch } = useApi()
const { isGlobalAdmin } = useAuth()
const { t, locale } = useI18n()

const pages = ref<StatusPageModel[]>([])
const users = ref<User[]>([])
const roles = ref<Role[]>([])
const isEditing = ref(false)
const showUsers = ref<StatusPageModel | null>(null) // status page whose users are managed
const currentEditItem = ref<'new' | number | null>(null)
const formData = ref<StatusPageForm>({})

// selectable page layouts (values as models.StatusPage.PageLayout).
const pageLayoutOptions = [
  { value: 'standard', label: 'statusPages.pageLayoutStandard', desc: 'statusPages.pageLayoutStandardDesc' },
  { value: 'liste', label: 'statusPages.pageLayoutListe', desc: 'statusPages.pageLayoutListeDesc' },
]

// Slug auto-generation from the name: as long as the user has not touched
// the slug of a new status page, it is derived live from the name - the same
// logic as slugify() in the backend (backend/handlers/statuspage.go), only as
// a preview; the final, collision-safe generation happens on the server if
// the field is still empty on save.
const slugTouched = ref(false)

// Design preview: the real public page in a modal, rendered with the unsaved
// design values of the form (StatusPage preview mode, read-only). Only for
// saved pages, because the page content comes from the public dashboard of
// the saved slug.
const previewOpen = ref(false)
const previewSlug = computed(() => pages.value.find(p => p.ID === currentEditItem.value)?.Slug || '')
const designPreview = computed(() => ({
  LogoURL: formData.value.LogoURL || '',
  PrimaryColor: formData.value.PrimaryColor || '',
  SecondaryColor: formData.value.SecondaryColor || '',
  HeaderStyle: formData.value.HeaderStyle || 'classic',
  PageLayout: formData.value.PageLayout || 'standard',
}))
function slugify(name: string | undefined) {
  const umlauts: Record<string, string> = { 'ä': 'ae', 'ö': 'oe', 'ü': 'ue', 'ß': 'ss' }
  return (name || '')
    .toLowerCase()
    .replace(/[äöüß]/g, ch => umlauts[ch]!)
    .split(/[^a-z0-9]+/)
    .filter(Boolean)
    .join('-')
}
watch(() => formData.value.Name, (name) => {
  if (currentEditItem.value === 'new' && !slugTouched.value) {
    formData.value.Slug = slugify(name)
  }
})

// Selected user and role ('' = none yet); the selects bind the numeric IDs.
const userAssignData = ref<{ user_id: number | string, role_id: number | string }>({ user_id: '', role_id: '' })
const pageUsers = ref<PageUserAssignment[]>([])
// open invitations of the page (invited by email, not yet accepted).
const pageInvitations = ref<PageInvitation[]>([])
const revokingInvitationId = ref<number | null>(null)
const alertMsg = ref('')
// Success messages carry the type "success" (they dismiss themselves), everything else stays a warning;
// clearAlert removes the message of the previous step.
const successMsg = ref('')
const alertType = computed(() => (alertMsg.value && alertMsg.value === successMsg.value ? 'success' : 'warning'))
const clearAlert = () => { alertMsg.value = '' }
const showSuccess = (msg: string) => {
  successMsg.value = msg
  alertMsg.value = msg
}
const isSaving = ref(false)
const deletingId = ref<number | null>(null)
const isAssigning = ref(false)
const removingUserId = ref<number | null | undefined>(null)

// Monitoring webhook token: the backend returns the plain-text token only
// right after generating it - here it is held only transiently in state,
// never stored in formData/pages or reloaded.
const webhookToken = ref('')
const isGeneratingToken = ref(false)
const isRevokingToken = ref(false)
const tokenCopied = ref(false)

// Custom domain: runs via its own endpoints instead of the generic save form,
// so that a domain change always reliably resets the verification status (see
// the backend comment in statuspage.go). customDomain* holds the state last
// confirmed by the backend, customDomainInput the not yet saved input field.
const customDomainInput = ref('')
const customDomainStatus = ref('')
const cnameTarget = ref('')
const customDomainMessage = ref<string | undefined>('')
const isSavingDomain = ref(false)
const isVerifyingDomain = ref(false)
const isRemovingDomain = ref(false)
// TXT owner verification: txtRecordValue only comes directly from the
// set/verify response (CustomDomainVerificationToken is json:"-" in the
// backend, like the WebhookToken), whereas customDomainVerifiedAt comes
// normally from the page data (not hidden) and therefore survives reopening
// the form without another save/verify call.
const txtRecordValue = ref('')
const customDomainVerifiedAt = ref<string | null>(null)
// Last (failed) DNS check: time and reason code.
const customDomainCheckedAt = ref<string | null>(null)
const customDomainCheckError = ref('')
const txtCopied = ref(false)
// DNS hints: apex domains need ALIAS/ANAME/CNAME flattening, and after a successful
// TXT check the domain may not point to the CNAME target yet (null = not checked).
const apexDomain = ref(false)
const pointsToTarget = ref<boolean | null>(null)

// The CNAME target and, while the TXT proof is open, the TXT value come from the
// backend when the form opens (the TXT value is not part of the page data).
// Without an answer the target stays unknown instead of being guessed.
const loadCustomDomainInfo = async (pageId: number) => {
  try {
    const resp: CustomDomainResponse = await apiFetch(`/api/admin/pages/${pageId}/custom-domain`)
    if (currentEditItem.value !== pageId) return
    cnameTarget.value = resp.cname_target
    apexDomain.value = !!resp.apex_domain
    txtRecordValue.value = resp.txt_record_value || ''
  } catch {
    // The rest of the form works without it; the description shows "…".
  }
}

// Stage 4d: table columns and field hints. The hints are plain text
// (FormField knows no <strong> markup in the hint).
const pageColumns = computed(() => [
  { key: 'Name', label: t('statusPages.tableName') },
  { key: 'Slug', label: t('statusPages.tableSlug') },
  { key: 'visibility', label: t('statusPages.tableVisibility') },
  { key: 'CustomDomain', label: t('statusPages.tableCustomDomain') },
  { key: 'UpdatedAt', label: t('components.tableChanged') },
])
const userColumns = computed(() => [
  { key: 'user', label: t('statusPages.fieldUser') },
  { key: 'role', label: t('statusPages.fieldRoleOnPage') },
])
const invitationColumns = computed(() => [
  { key: 'email', label: t('statusPages.colInvitedEmail') },
  { key: 'role', label: t('statusPages.fieldRoleOnPage') },
  { key: 'expires', label: t('statusPages.colInvitationExpires') },
])
// Without a configured base domain (VITE_APP_BASE_DOMAINS) there is no subdomain:
// the status page is then reachable under /status/<slug>, and the default sender
// comes from the server.
const slugHint = computed(() => {
  const slug = formData.value.Slug || '…'
  const base = primaryBaseDomain()
  return base
    ? `${t('statusPages.slugHintPrefix')} ${slug}.${base} ${t('statusPages.slugHintSuffix')}`
    : t('statusPages.slugHintPath', { slug })
})

// Direction C: page header, search, row menu and form errors at the field.
const breadcrumbs = usePageCrumbs(computed(() => t('adminNav.items.pages')), { global: true })
const subtitle = computed(() => t('statusPages.subtitle', {
  pages: t('statusPages.countPages', { n: pages.value.length }, pages.value.length),
  priv: t('statusPages.countPrivate', { n: privateCount.value }, privateCount.value),
}))
const privateCount = computed(() => pages.value.filter(p => p.IsPrivate).length)
const query = ref('')
const searching = computed(() => query.value.trim() !== '')
const searchText = (p: StatusPageModel) => `${p.Name} ${p.Slug} ${p.Description || ''} ${p.CustomDomain || ''}`.toLowerCase()
const rows = computed(() => {
  const q = query.value.trim().toLowerCase()
  return q ? pages.value.filter(p => searchText(p).includes(q)) : pages.value
})
const formTitle = computed(() => {
  if (currentEditItem.value === 'new') return t('statusPages.createTitle')
  return t('statusPages.editTitle', { name: formData.value.Name })
})
const formErrors = ref({ name: '', slug: '' })
// The slug of the system page is fixed (backend: system_page_slug_locked).
const isSystemPageEdit = computed(() => currentEditItem.value !== 'new' && !!formData.value.IsSystem)
const slugFieldHint = computed(() => {
  if (currentEditItem.value === 'new') return slugHint.value
  return isSystemPageEdit.value ? t('statusPages.systemSlugLocked') : null
})
const formEl = ref<HTMLFormElement | null>(null)

const menuItems = (p: StatusPageModel) => [
  { key: 'open', label: t('statusPages.openAction'), icon: 'globe' },
  { key: 'users', label: t('statusPages.usersAction'), icon: 'users' },
  ...(isGlobalAdmin.value ? [
    { key: 'edit', label: t('common.edit'), icon: 'pencil' },
    // The system page cannot be deleted (backend: 409).
    ...(p.IsSystem ? [] : [{ key: 'delete', label: deletingId.value === p.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === p.ID }]),
  ] : []),
]
const onMenuSelect = (p: StatusPageModel, key: string) => {
  if (key === 'open') openPublicPage(p)
  else if (key === 'users') openUserManagement(p)
  else if (key === 'edit') openEdit(p)
  else deleteItem(p.ID, p.Name)
}

const loadData = async () => {
  try {
    const [p, u, r] = await Promise.all([
      apiFetch<StatusPageModel[]>('/api/pages'),
      apiFetch<User[]>('/api/users'),
      apiFetch<Role[]>('/api/roles'),
    ])
    pages.value = p
    users.value = u
    roles.value = r
  } catch (err) {
    alertMsg.value = t('statusPages.loadError') + (err as Error).message
  }
}

const loadPageUsers = async (pageId: number) => {
  // H6: load both lists independently; if only the invitation list fails,
  // the member list stays visible. The state is only set after the awaits and
  // only if the same page is still open (otherwise the late response of a
  // previously opened page would overwrite the current one).
  const [members, invitations] = await Promise.allSettled([
    apiFetch<PageUserAssignment[]>(`/api/admin/pages/${pageId}/users`),
    apiFetch<PageInvitation[]>(`/api/admin/pages/${pageId}/invitations`),
  ])
  if (showUsers.value?.ID !== pageId) return
  if (members.status === 'rejected') throw members.reason
  pageUsers.value = members.value
  pageInvitations.value = invitations.status === 'fulfilled' ? invitations.value : []
}

// Quick search deep link: ?edit=<page id> opens that page's edit form (the
// search then focuses the wanted field). The parameter is removed afterwards so
// the same link works again. Injected without warning when mounted without router.
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)
const openFromQuery = () => {
  const id = route?.query.edit
  if (id == null) return
  const page = pages.value.find(p => String(p.ID) === String(id))
  if (!page) return
  openEdit(page, { focus: false })
  const { edit: _edit, ...rest } = route!.query
  router?.replace({ query: rest })
}
watch(() => route?.query.edit, openFromQuery)

onMounted(async () => {
  await loadData()
  openFromQuery()
})

const openEdit = (item: StatusPageModel | 'new', { focus = true }: { focus?: boolean } = {}) => {
  formData.value = item === 'new'
    ? {
      // Stryker disable next-line StringLiteral: the name watcher overwrites the slug right on opening (undefined -> ''), so the initial value is not observable
      Slug: '',
      Name: '',
      Description: '',
      IsPrivate: false,
      DefaultRoleID: null,
      AutoJoinEmailDomains: '',
      LogoURL: '',
      PrimaryColor: '',
      SecondaryColor: '',
      HeaderStyle: 'classic',
      PageLayout: 'standard',
      // Stryker disable next-line StringLiteral: the slug watcher re-derives the address right on opening, so the initial value is not observable
      NotificationFromAddress: ''
    }
    : JSON.parse(JSON.stringify(item))
  currentEditItem.value = item === 'new' ? 'new' : item.ID
  slugTouched.value = false
  previewOpen.value = false
  webhookToken.value = ''
  tokenCopied.value = false
  customDomainInput.value = item === 'new' ? '' : (item.CustomDomain || '')
  customDomainStatus.value = item === 'new' ? '' : (item.CustomDomainStatus || '')
  customDomainMessage.value = ''
  txtRecordValue.value = ''
  customDomainVerifiedAt.value = item === 'new' ? null : (item.CustomDomainVerifiedAt || null)
  customDomainCheckedAt.value = item === 'new' ? null : (item.CustomDomainCheckedAt || null)
  customDomainCheckError.value = item === 'new' ? '' : (item.CustomDomainCheckError || '')
  cnameTarget.value = ''
  apexDomain.value = false
  pointsToTarget.value = null
  if (item !== 'new') loadCustomDomainInfo(item.ID)
  formErrors.value = { name: '', slug: '' }
  showUsers.value = null
  isEditing.value = true
  if (focus) focusForm()
}

// Open the form: focus on the name field; below 860 px the form sits below
// the list and is scrolled into view.
const focusForm = async () => {
  await nextTick()
  formEl.value?.closest('aside')?.scrollIntoView?.({ block: 'nearest' })
  formEl.value?.querySelector('input')?.focus({ preventScroll: true })
}

const cancelEdit = () => {
  isEditing.value = false
  previewOpen.value = false
  currentEditItem.value = null
  formData.value = {}
  webhookToken.value = ''
  tokenCopied.value = false
  customDomainInput.value = ''
  customDomainStatus.value = ''
  customDomainMessage.value = ''
  txtRecordValue.value = ''
  customDomainVerifiedAt.value = null
  customDomainCheckedAt.value = null
  customDomainCheckError.value = ''
  apexDomain.value = false
  pointsToTarget.value = null
}

const customDomainStatusLabel = (status: string | undefined) => ({
  pending:  t('statusPages.domainPending'),
  check_failed: t('statusPages.domainCheckFailed'),
  verified: t('statusPages.domainVerified'),
  active:   t('statusPages.domainActive'),
} as Record<string, string>)[status as string] ?? t('statusPages.domainNone')

// Domain status to the status level of the StatusBadge: pending = warning,
// verified = notice (maintenance shape), active = fine; anything unknown stays neutral.
// Reason of the last failed check (backend code -> text); unknown codes show
// the general text.
const customDomainCheckReason = computed(() => ({
  txt_not_found: t('statusPages.checkReasonTxtNotFound'),
  dns_lookup_failed: t('statusPages.checkReasonLookupFailed'),
} as Record<string, string>)[customDomainCheckError.value] ?? t('statusPages.checkReasonUnknown'))

const DOMAIN_LEVEL: Record<string, string> = { pending: 'degraded', check_failed: 'major', verified: 'maintenance', active: 'operational' }
const domainStatusLevel = (status: string | undefined) => DOMAIN_LEVEL[status as string] ?? null

const saveCustomDomain = async () => {
  isSavingDomain.value = true
  customDomainMessage.value = ''
  txtRecordValue.value = ''
  try {
    const resp: CustomDomainResponse = await apiFetch(`/api/admin/pages/${currentEditItem.value}/custom-domain`, {
      method: 'PUT',
      body: JSON.stringify({ domain: customDomainInput.value })
    })
    customDomainStatus.value = resp.custom_domain_status
    cnameTarget.value = resp.cname_target
    apexDomain.value = !!resp.apex_domain
    pointsToTarget.value = null
    txtRecordValue.value = resp.txt_record_value || ''
    await loadData()
  } catch (err) {
    alertMsg.value = t('statusPages.domainSaveError') + (err as Error).message
  } finally {
    isSavingDomain.value = false
  }
}

const verifyCustomDomain = async () => {
  isVerifyingDomain.value = true
  customDomainMessage.value = ''
  txtRecordValue.value = ''
  try {
    const resp: CustomDomainResponse = await apiFetch(`/api/admin/pages/${currentEditItem.value}/custom-domain/verify`, { method: 'POST' })
    customDomainStatus.value = resp.custom_domain_status
    customDomainCheckedAt.value = resp.custom_domain_checked_at || null
    customDomainCheckError.value = resp.custom_domain_check_error || ''
    customDomainMessage.value = resp.message
    cnameTarget.value = resp.cname_target
    apexDomain.value = !!resp.apex_domain
    pointsToTarget.value = resp.points_to_target ?? null
    txtRecordValue.value = resp.txt_record_value || ''
    if (resp.verified_at) {
      customDomainVerifiedAt.value = resp.verified_at
    }
    await loadData()
  } catch (err) {
    alertMsg.value = t('statusPages.domainVerifyError') + (err as Error).message
  } finally {
    isVerifyingDomain.value = false
  }
}

const removeCustomDomain = async () => {
  // Capture the target before the dialog (the selection can change meanwhile).
  const pageId = currentEditItem.value
  if (!(await confirmDialog({ message: t('statusPages.confirmRemoveDomain'), confirmLabel: t('confirm.remove') }))) return
  isRemovingDomain.value = true
  try {
    await apiFetch(`/api/admin/pages/${pageId}/custom-domain`, { method: 'DELETE' })
    customDomainInput.value = ''
    customDomainStatus.value = ''
    customDomainMessage.value = ''
    apexDomain.value = false
    pointsToTarget.value = null
    await loadData()
  } catch (err) {
    alertMsg.value = t('statusPages.domainRemoveError') + (err as Error).message
  } finally {
    isRemovingDomain.value = false
  }
}

const generateWebhookToken = async () => {
  // Capture the target before the dialog (the selection can change meanwhile).
  const pageId = currentEditItem.value
  if (!(await confirmDialog({ message: t('statusPages.confirmGenerateToken'), confirmLabel: t('confirm.generate') }))) return
  isGeneratingToken.value = true
  tokenCopied.value = false
  try {
    const resp: { token: string } = await apiFetch(`/api/admin/pages/${pageId}/webhook-token`, { method: 'POST' })
    webhookToken.value = resp.token
  } catch (err) {
    alertMsg.value = t('statusPages.tokenGenerateError') + (err as Error).message
  } finally {
    isGeneratingToken.value = false
  }
}

const revokeWebhookToken = async () => {
  // Capture the target before the dialog (the selection can change meanwhile).
  const pageId = currentEditItem.value
  if (!(await confirmDialog({ message: t('statusPages.confirmRevokeToken'), confirmLabel: t('confirm.revoke') }))) return
  isRevokingToken.value = true
  try {
    await apiFetch(`/api/admin/pages/${pageId}/webhook-token`, { method: 'DELETE' })
    webhookToken.value = ''
    tokenCopied.value = false
  } catch (err) {
    alertMsg.value = t('statusPages.tokenRevokeError') + (err as Error).message
  } finally {
    isRevokingToken.value = false
  }
}

const copyWebhookToken = async () => {
  try {
    await navigator.clipboard.writeText(webhookToken.value)
    tokenCopied.value = true
    setTimeout(() => { tokenCopied.value = false }, 2000)
  } catch (err) {
    alertMsg.value = t('statusPages.copyError') + (err as Error).message
  }
}

const copyTxtRecord = async () => {
  try {
    await navigator.clipboard.writeText(txtRecordValue.value)
    txtCopied.value = true
    setTimeout(() => { txtCopied.value = false }, 2000)
  } catch (err) {
    alertMsg.value = t('statusPages.copyError') + (err as Error).message
  }
}

// Input errors of the backend (400 with a code) appear at the field.
const onSaveError = async (err: ApiError) => {
  const text = serverInputErrorText(err, t)
  const field = ({ name_required: 'name', slug_reserved: 'slug', slug_taken: 'slug' } as Record<string, 'name' | 'slug'>)[err.data?.code as string]
  if (!text || !field) {
    alertMsg.value = t('statusPages.saveError') + err.message
    return
  }
  formErrors.value = { ...formErrors.value, [field]: text }
  await nextTick()
  formEl.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
}

const saveAction = useSaveAction({ apiFetch, isSaving, reload: loadData, onStart: clearAlert, onError: onSaveError })
const deleteAction = useDeleteAction({ apiFetch, deletingId, reload: loadData, onStart: clearAlert, onError: (err) => { alertMsg.value = t('statusPages.deleteError') + err.message } })

const saveItem = async () => {
  const isNew = currentEditItem.value === 'new'
  // Required fields: error at the field instead of a server error, focus on the first invalid field.
  formErrors.value = {
    name: (formData.value.Name || '').trim() ? '' : t('statusPages.nameRequired'),
    slug: !isNew && !(formData.value.Slug || '').trim() ? t('statusPages.slugRequired') : '',
  }
  if (formErrors.value.name || formErrors.value.slug) {
    await nextTick()
    formEl.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
    return
  }
  const endpoint = `/api/admin/pages${isNew ? '' : '/' + currentEditItem.value}`
  const payload = { ...formData.value }
  payload.DefaultRoleID = payload.DefaultRoleID ? Number.parseInt(String(payload.DefaultRoleID)) : null

  await saveAction(endpoint, isNew ? 'POST' : 'PUT', payload, cancelEdit)
}

const deleteItem = (id: number, name: string) =>
  deleteAction(`/api/admin/pages/${id}`, id, t('statusPages.confirmDelete', { name }))

const openUserManagement = async (page: StatusPageModel) => {
  cancelEdit()
  showUsers.value = page
  await loadPageUsers(page.ID)
}

const openPublicPage = (page: StatusPageModel) => {
  window.open('/status/' + page.Slug, '_blank')
}

const assignUser = async () => {
  if (!userAssignData.value.user_id || !userAssignData.value.role_id) return
  isAssigning.value = true
  try {
    await apiFetch(`/api/admin/pages/${showUsers.value!.ID}/users`, {
      method: 'POST',
      body: JSON.stringify({
        user_id: Number.parseInt(String(userAssignData.value.user_id)),
        role_id: Number.parseInt(String(userAssignData.value.role_id)),
      })
    })
    userAssignData.value = { user_id: '', role_id: '' }
    await loadPageUsers(showUsers.value!.ID)
  } catch (err) {
    alertMsg.value = t('statusPages.assignError') + userAdminErrorText(err, t)
  } finally {
    isAssigning.value = false
  }
}

// Revoke an invitation; the link in the mail becomes invalid.
const revokeInvitation = async (invitation: PageInvitation) => {
  const pageId = showUsers.value?.ID
  if (!(await confirmDialog({ message: t('statusPages.confirmRevokeInvitation', { email: invitation.Email }), confirmLabel: t('statusPages.revokeInvitation') }))) return
  clearAlert()
  revokingInvitationId.value = invitation.ID
  try {
    await apiFetch(`/api/admin/pages/${pageId}/invitations/${invitation.ID}`, { method: 'DELETE' })
    showSuccess(t('statusPages.invitationRevoked'))
    await loadPageUsers(pageId!)
  } catch (err) {
    alertMsg.value = t('statusPages.revokeInvitationError') + (err as Error).message
  } finally {
    revokingInvitationId.value = null
  }
}

const removeUser = async (assignment: PageUserAssignment) => {
  // Deleting asks for confirmation everywhere else; removing the assignment did not.
  const userId = assignment?.User?.ID
  const name = assignment?.User?.Email || '–'
  const pageId = showUsers.value?.ID
  if (!(await confirmDialog({ message: t('adminFeedback.confirmRemoveUser', { name }), confirmLabel: t('confirm.remove') }))) return
  clearAlert()
  removingUserId.value = userId
  try {
    await apiFetch(`/api/admin/pages/${pageId}/users/${userId}`, { method: 'DELETE' })
    showSuccess(t('adminFeedback.userRemoved'))
    await loadPageUsers(pageId!)
  } catch (err) {
    alertMsg.value = t('statusPages.removeUserError') + (err as Error).message
  } finally {
    removingUserId.value = null
  }
}
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} mb-16`">
      {{ alertMsg }} <button class="alert-close" @click="alertMsg=''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('adminNav.items.pages')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="isGlobalAdmin" #actions>
        <OnyxButton :label="t('statusPages.newPageButton')" @click="openEdit('new')" />
      </template>
    </PageHeader>

    <!-- Direction C: list on the left, form or user management as a
         side card on the right (below it under 860 px). -->
    <div class="split" :class="{ 'split--open': isEditing || showUsers }">
      <ListCard :title="t('statusPages.cardTitle')">
        <template #tools>
          <SearchBox v-model="query" :label="t('statusPages.searchLabel')" :placeholder="t('statusPages.searchPlaceholder')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('statusPages.searchResult', { n: rows.length }, rows.length) }}</p>

        <EmptyState v-if="pages.length === 0" :title="t('statusPages.noPagesFound')" icon="globe" />
        <EmptyState v-else-if="rows.length === 0" :title="t('statusPages.noResults')" :text="t('statusPages.noResultsText')" icon="search" />
        <ResponsiveTable v-else data-table :columns="pageColumns" :items="rows" row-key="ID">
          <template #cell-Name="{ item }">
            <div class="component-name">
              <strong class="component-name__title">{{ item.Name }}</strong>
              <small v-if="item.Description" class="component-name__desc">{{ item.Description }}</small>
            </div>
          </template>
          <template #cell-Slug="{ item }"><span class="meta-tag meta-tag--slug">{{ item.Slug }}</span></template>
          <template #cell-visibility="{ item }">
            <span class="meta-tag">
              <AppIcon :name="item.IsPrivate ? 'lock' : 'globe'" :size="12" />
              {{ item.IsPrivate ? t('statusPages.visibilityPrivate') : t('statusPages.visibilityPublic') }}
            </span>
          </template>
          <template #cell-CustomDomain="{ item }">
            <template v-if="item.CustomDomain">
              {{ item.CustomDomain }}
              <StatusBadge class="domain-badge" :status="domainStatusLevel(item.CustomDomainStatus)" :label="customDomainStatusLabel(item.CustomDomainStatus)" />
            </template>
            <template v-else>–</template>
          </template>
          <template #cell-UpdatedAt="{ item }">
            <RelativeTime :value="item.UpdatedAt" />
          </template>
          <template #actions="{ item }">
            <RowMenu :label="t('statusPages.menuLabel', { name: item.Name })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
      </ListCard>

      <!-- Form -->
      <FormPanel v-if="isEditing" class="form-panel--tall" :title="formTitle" @close="cancelEdit">
        <!-- saveItem checks name/slug itself (message at the field); the submit guard
             should not interfere. -->
        <form ref="formEl" novalidate data-self-validates class="edit-form" @submit.prevent="saveItem">
          <FormField v-slot="f" :label="t('statusPages.fieldName')" required field-id="page-name" :error="formErrors.name || null">
            <input :id="f.id" v-model="formData.Name" class="form-input" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('statusPages.namePlaceholder')" @input="formErrors.name = ''" />
          </FormField>
          <FormField
            v-slot="f"
            :label="t('statusPages.fieldSlug')"
            :required="currentEditItem !== 'new'"
            :hint="slugFieldHint"
            :error="formErrors.slug || null"
            field-id="page-slug"
          >
            <input
              :id="f.id"
              v-model="formData.Slug"
              class="form-input"
              :required="f.required"
              :disabled="isSystemPageEdit"
              :aria-invalid="f.invalid"
              :aria-describedby="f.describedby"
              :placeholder="t('statusPages.slugPlaceholder')"
              @input="slugTouched = true; formErrors.slug = ''"
            />
          </FormField>
          <FormField v-slot="f" :label="t('statusPages.fieldDescription')" field-id="page-description">
            <textarea :id="f.id" v-model="formData.Description" class="form-textarea" rows="3" :placeholder="t('statusPages.descriptionPlaceholder')"></textarea>
          </FormField>
          <FormField v-slot="f" :label="t('statusPages.fieldLogoUrl')" field-id="page-logo-url">
            <input :id="f.id" v-model="formData.LogoURL" class="form-input" type="url" placeholder="https://meinefirma.de/logo.png" />
          </FormField>

          <div class="color-grid">
            <FormField v-slot="f" :label="t('statusPages.fieldPrimaryColor')" field-id="page-primary-color">
              <div class="adm-row adm-row--nowrap">
                <input :id="f.id" type="color" v-model="formData.PrimaryColor" class="color-swatch" />
                <input v-model="formData.PrimaryColor" class="form-input" :aria-label="t('statusPages.primaryColorHexLabel')" :placeholder="t('statusPages.primaryColorPlaceholder')" />
              </div>
            </FormField>
            <FormField v-slot="f" :label="t('statusPages.fieldSecondaryColor')" field-id="page-secondary-color">
              <div class="adm-row adm-row--nowrap">
                <input :id="f.id" type="color" v-model="formData.SecondaryColor" class="color-swatch" />
                <input v-model="formData.SecondaryColor" class="form-input" :aria-label="t('statusPages.secondaryColorHexLabel')" :placeholder="t('statusPages.secondaryColorPlaceholder')" />
              </div>
            </FormField>
          </div>
          <p class="adm-hint color-hint">{{ t('statusPages.colorHint') }}</p>

          <FormField v-slot="f" :label="t('statusPages.fieldHeaderStyle')" field-id="page-header-style">
            <select :id="f.id" v-model="formData.HeaderStyle" class="form-select">
              <option value="classic">{{ t('statusPages.headerClassic') }}</option>
              <option value="centered">{{ t('statusPages.headerCentered') }}</option>
              <option value="minimal">{{ t('statusPages.headerMinimal') }}</option>
            </select>
          </FormField>

          <!-- Page layout with a small preview per variant -->
          <fieldset id="page-layout" class="form-group layout-picker">
            <legend>{{ t('statusPages.fieldPageLayout') }}</legend>
            <label
              v-for="opt in pageLayoutOptions"
              :key="opt.value"
              class="layout-option"
              :class="{ 'layout-option--active': formData.PageLayout === opt.value }"
            >
              <input type="radio" name="page-layout" :value="opt.value" v-model="formData.PageLayout" />
              <span class="layout-preview" :class="`layout-preview--${opt.value}`" aria-hidden="true">
                <span class="lp-bar lp-bar--status"></span>
                <span class="lp-bar"></span>
                <span class="lp-bar"></span>
              </span>
              <span class="layout-option-text">
                <strong>{{ t(opt.label) }}</strong>
                <small>{{ t(opt.desc) }}</small>
              </span>
            </label>
          </fieldset>

          <!-- Preview of the design fields above in a modal; nothing is saved here. -->
          <div class="design-preview">
            <button
              v-if="currentEditItem !== 'new' && previewSlug"
              type="button"
              class="action-btn"
              data-test="design-preview-open"
              aria-haspopup="dialog"
              @click="previewOpen = true"
            >
              <AppIcon name="globe" :size="14" /> {{ t('statusPages.previewShow') }}
            </button>
            <p v-else class="adm-hint" data-test="design-preview-new">{{ t('statusPages.previewNewPage') }}</p>
          </div>

          <FormField v-slot="f" :label="t('statusPages.fieldNotificationFrom')" :hint="t('statusPages.fromAddressHint')" field-id="page-notification-from">
            <input
              :id="f.id"
              v-model="formData.NotificationFromAddress"
              class="form-input"
              :aria-describedby="f.describedby"
              :placeholder="t('statusPages.notificationFromPlaceholder')"
            />
          </FormField>

          <div class="checkbox-row">
            <input type="checkbox" id="isPrivate" v-model="formData.IsPrivate" />
            <label for="isPrivate">{{ t('statusPages.fieldIsPrivate') }}</label>
          </div>

          <div v-if="formData.IsPrivate" class="private-options">
            <FormField v-slot="f" :label="t('statusPages.fieldDefaultRole')" :hint="t('statusPages.defaultRoleHint')" field-id="page-default-role">
              <select :id="f.id" v-model="formData.DefaultRoleID" class="form-select" :aria-describedby="f.describedby">
                <option :value="null">{{ t('statusPages.defaultRoleDenied') }}</option>
                <option v-for="r in pageRoleOptions(roles)" :key="r.ID" :value="r.ID">{{ r.Name }}</option>
              </select>
            </FormField>
            <FormField v-slot="f" :label="t('statusPages.fieldAutoJoinDomains')" :hint="t('statusPages.autoJoinDomainsHint')" field-id="page-autojoin-domains">
              <input :id="f.id" v-model="formData.AutoJoinEmailDomains" class="form-input" :aria-describedby="f.describedby" :placeholder="t('statusPages.autoJoinDomainsPlaceholder')" />
            </FormField>
          </div>

          <div class="form-actions adm-actions-spaced">
            <OnyxButton :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>

        <OnyxModal
          v-model:open="previewOpen"
          class="design-preview-modal"
          :label="t('statusPages.previewTitle', { name: formData.Name || previewSlug })"
        >
          <template #description>
            <p class="adm-hint design-preview-hint" data-test="design-preview-hint">{{ t('statusPages.previewHint') }}</p>
          </template>
          <div v-if="previewOpen" class="design-preview-frame theme-public">
            <StatusPage :slug="previewSlug" :preview="designPreview" />
          </div>
        </OnyxModal>

        <!-- Monitoring webhook token: only for pages already saved; -->
        <!-- a "new" page has no ID yet to generate a token against. -->
        <section v-if="currentEditItem !== 'new'" id="page-webhook" class="adm-section">
          <h4 class="adm-subhead">{{ t('statusPages.webhookHeading') }}</h4>
          <p class="section-text">
            {{ t('statusPages.webhookDescPrefix') }}
            <code>POST /api/webhooks/components/&lt;component_id&gt;/status</code> {{ t('statusPages.webhookDescMid') }}
            <code>X-Webhook-Token</code> {{ t('statusPages.webhookDescSuffix') }}
          </p>

          <FormField v-if="webhookToken" v-slot="f" :label="t('statusPages.newTokenLabel')" field-id="webhook-token-display">
            <div class="adm-row adm-row--nowrap">
              <input :id="f.id" :value="webhookToken" class="form-input token-input" readonly @focus="($event.target as HTMLInputElement).select()" />
              <button type="button" class="action-btn" @click="copyWebhookToken">
                <AppIcon :name="tokenCopied ? 'check-circle' : 'clipboard'" :size="14" /> {{ tokenCopied ? t('common.copied') : t('common.copy') }}
              </button>
            </div>
          </FormField>

          <div class="adm-row">
            <button type="button" class="action-btn" @click="generateWebhookToken" :disabled="isGeneratingToken">
              <AppIcon name="key" :size="14" /> {{ isGeneratingToken ? t('statusPages.generatingToken') : (webhookToken ? t('statusPages.regenerateToken') : t('statusPages.generateToken')) }}
            </button>
            <button type="button" class="action-btn delete-btn" @click="revokeWebhookToken" :disabled="isRevokingToken">
              <AppIcon name="trash" :size="14" /> {{ isRevokingToken ? t('statusPages.revokingToken') : t('statusPages.revokeToken') }}
            </button>
          </div>
        </section>

        <!-- Custom domain -->
        <section v-if="currentEditItem !== 'new'" class="adm-section">
          <h4 class="adm-subhead">{{ t('statusPages.customDomainHeading') }}</h4>
          <p class="section-text" data-test="custom-domain-desc">
            {{ t('statusPages.customDomainDescPrefix') }} <code>status.example.org</code> {{ t('statusPages.customDomainDescMid') }}
            <code>{{ cnameTarget || '…' }}</code> {{ t('statusPages.customDomainDescSuffix') }}
          </p>

          <FormField v-slot="f" :label="t('statusPages.fieldDomain')" field-id="custom-domain-input">
            <div class="adm-row adm-row--nowrap">
              <input :id="f.id" v-model="customDomainInput" class="form-input" :placeholder="t('statusPages.domainPlaceholder')" />
              <button type="button" class="action-btn" @click="saveCustomDomain" :disabled="isSavingDomain">
                {{ isSavingDomain ? t('common.saving') : t('common.save') }}
              </button>
            </div>
          </FormField>

          <p v-if="apexDomain" class="section-text section-text--small" data-test="apex-domain-hint">
            {{ t('statusPages.apexDomainHint') }}
          </p>

          <p v-if="customDomainStatus" class="status-line">
            <StatusBadge :status="domainStatusLevel(customDomainStatus)" :label="customDomainStatusLabel(customDomainStatus)" />
          </p>

          <!-- TXT verification: first confirm the TXT record (DNS ownership), then the CNAME -->
          <div v-if="['pending', 'check_failed'].includes(customDomainStatus) && txtRecordValue" class="txt-box">
            <p class="txt-label">{{ t('statusPages.txtRecordLabel') }}</p>
            <div class="adm-row adm-row--grow adm-row--nowrap">
              <code class="txt-code">{{ txtRecordValue }}</code>
              <button type="button" class="action-btn" @click="copyTxtRecord" :title="t('common.copy')">
                <AppIcon :name="txtCopied ? 'check-circle' : 'clipboard'" :size="14" />
              </button>
            </div>
            <p class="section-text section-text--small">{{ t('statusPages.txtRecordInstructions', { domain: customDomainInput }) }}</p>
          </div>

          <!-- Last check error: with verified/active it does not change the status, the domain stays in operation -->
          <p v-if="customDomainCheckError && customDomainCheckedAt" class="section-text section-text--small" data-test="domain-check-failed">
            {{ t('statusPages.checkFailedAt', { date: new Date(customDomainCheckedAt).toLocaleString(), reason: customDomainCheckReason }) }}
            <template v-if="['verified', 'active'].includes(customDomainStatus)"> {{ t('statusPages.checkFailedKept') }}</template>
          </p>

          <p v-if="customDomainMessage" class="section-text">{{ customDomainMessage }}</p>

          <p v-if="pointsToTarget === false" class="section-text section-text--small" data-test="domain-not-pointing">
            {{ t('statusPages.domainNotPointing', { target: cnameTarget }) }}
          </p>

          <p v-if="customDomainVerifiedAt && customDomainStatus === 'verified'" class="section-text section-text--small">
            {{ t('statusPages.verifiedAt', { date: new Date(customDomainVerifiedAt).toLocaleString() }) }}
          </p>

          <div v-if="customDomainStatus" class="adm-row">
            <button type="button" class="action-btn" @click="verifyCustomDomain" :disabled="isVerifyingDomain">
              <AppIcon name="check-circle" :size="14" /> {{ isVerifyingDomain ? t('statusPages.verifyingDomain') : t('statusPages.verifyDomain') }}
            </button>
            <button type="button" class="action-btn delete-btn" @click="removeCustomDomain" :disabled="isRemovingDomain">
              <AppIcon name="trash" :size="14" /> {{ isRemovingDomain ? t('statusPages.removingDomain') : t('statusPages.removeDomainAction') }}
            </button>
          </div>
        </section>
      </FormPanel>

      <!-- User management -->
      <FormPanel v-else-if="showUsers" class="form-panel--tall" :title="t('statusPages.permissionsTitle', { name: showUsers.Name })" :show-required-hint="false" @close="showUsers = null">
        <!-- Assign users -->
        <div class="edit-form assign-block">
          <h4 class="adm-subhead">{{ t('statusPages.addUserHeading') }}</h4>
          <div class="assign-grid">
            <FormField v-slot="f" :label="t('statusPages.fieldUser')" field-id="assign-user">
              <select :id="f.id" v-model="userAssignData.user_id" class="form-select">
                <option value="">{{ t('statusPages.selectUserPlaceholder') }}</option>
                <option v-for="u in users" :key="u.ID" :value="u.ID">{{ u.Email }}</option>
              </select>
            </FormField>
            <FormField v-slot="f" :label="t('statusPages.fieldRoleOnPage')" field-id="assign-role">
              <select :id="f.id" v-model="userAssignData.role_id" class="form-select">
                <option value="">{{ t('statusPages.selectRolePlaceholder') }}</option>
                <option v-for="r in pageRoleOptions(roles)" :key="r.ID" :value="r.ID">{{ r.Name }}</option>
              </select>
            </FormField>
            <OnyxButton :label="isAssigning ? t('common.adding') : t('statusPages.addUserButton')" @click="assignUser" :disabled="isAssigning" />
          </div>
        </div>

        <!-- Current users -->
        <EmptyState v-if="pageUsers.length === 0" :title="t('statusPages.noUsersAssigned')" icon="users" />
        <ResponsiveTable v-else data-table :columns="userColumns" :items="pageUsers" row-key="ID">
          <template #cell-user="{ item }">{{ item.User?.Email || '–' }}</template>
          <template #cell-role="{ item }"><span class="meta-tag">{{ item.Role?.Name || '–' }}</span></template>
          <template #actions="{ item }">
            <button type="button" class="action-btn delete-btn" :aria-label="t('statusPages.removeUserLabel', { name: item.User?.Email || '–' })" @click="removeUser(item)" :disabled="removingUserId === item.User?.ID">
              <AppIcon name="trash" :size="14" /> {{ removingUserId === item.User?.ID ? t('common.removing') : t('common.remove') }}
            </button>
          </template>
        </ResponsiveTable>

        <!-- open invitations -->
        <h4 class="adm-subhead invitations-heading">{{ t('statusPages.invitationsHeading') }}</h4>
        <p class="section-text section-text--small">{{ t('statusPages.invitationsHint') }}</p>
        <EmptyState v-if="pageInvitations.length === 0" :title="t('statusPages.noInvitations')" icon="mail" />
        <ResponsiveTable v-else data-table class="invitations-table" :columns="invitationColumns" :items="pageInvitations" row-key="ID">
          <template #cell-email="{ item }">{{ item.Email }}</template>
          <template #cell-role="{ item }"><span class="meta-tag">{{ item.RoleName || '–' }}</span></template>
          <template #cell-expires="{ item }"><RelativeTime :value="item.ExpiresAt" /></template>
          <template #actions="{ item }">
            <button type="button" class="action-btn delete-btn revoke-invitation-btn" :aria-label="t('statusPages.revokeInvitationLabel', { email: item.Email })" @click="revokeInvitation(item)" :disabled="revokingInvitationId === item.ID">
              <AppIcon name="x" :size="14" /> {{ revokingInvitationId === item.ID ? t('common.removing') : t('statusPages.revokeInvitation') }}
            </button>
          </template>
        </ResponsiveTable>
      </FormPanel>
    </div>
  </div>
</template>

<style scoped>
.adm-row--nowrap { flex-wrap: nowrap; }
.color-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: var(--sp-4); }
.color-swatch { inline-size: 40px; block-size: 34px; padding: 2px; flex-shrink: 0; }
/* The hex field takes the rest next to the colour swatch and may shrink. */
.color-grid .adm-row--nowrap > .form-input { flex: 1 1 0; min-inline-size: 0; }
.color-hint { margin: calc(-1 * var(--sp-2)) 0 var(--sp-4); }
.checkbox-row { display: flex; align-items: center; gap: var(--sp-2); margin-top: var(--sp-4); }
.checkbox-row label { cursor: pointer; font-size: var(--fs-base); }
.private-options { margin-top: var(--sp-3); padding-inline-start: var(--sp-5); border-inline-start: 2px solid var(--border); }
.section-text { margin: 0 0 var(--sp-3); font-size: var(--fs-sm); color: var(--text-2); }
.section-text--small { font-size: var(--fs-xs); margin-top: var(--sp-2); }
.token-input { font-family: var(--font-mono); }
.status-line { display: flex; align-items: center; gap: var(--sp-3); margin: 0 0 var(--sp-2); }
.txt-box { background: var(--surface-2); padding: var(--sp-3); border-radius: var(--r-sm); margin-bottom: var(--sp-3); }
.txt-label { margin: 0 0 6px; font-size: var(--fs-xs); font-weight: 600; color: var(--text-2); }
.txt-code { background: var(--bg); padding: var(--sp-2); border-radius: var(--r-sm); font-size: var(--fs-sm); overflow-x: auto; }
.assign-block { margin-bottom: var(--sp-5); }
.invitations-heading { margin-top: var(--sp-5); }
.assign-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--sp-3); justify-items: start; }
.assign-grid :deep(.form-field) { margin-bottom: 0; inline-size: 100%; }
/* The long domain badge ("Pending – DNS not checked yet") may wrap in the
   card (390 px) instead of being cut off on the right. */
.domain-badge { margin-inline-start: 6px; }
.domain-badge.st-badge { white-space: normal; min-inline-size: 0; text-align: start; }

.layout-picker { border: 0; padding: 0; margin: 0 0 var(--sp-4); min-width: 0; display: flex; flex-direction: column; gap: 8px; }
.layout-picker legend { font-size: var(--fs-sm); font-weight: 600; margin-bottom: 6px; padding: 0; color: var(--text); }
.layout-option {
  display: flex; align-items: center; gap: 12px; padding: 10px 12px; cursor: pointer;
  border: 1px solid var(--border); border-radius: var(--r-lg);
}
.layout-option--active { border-color: var(--primary); }
.layout-option:focus-within { outline: 2px solid var(--focus); outline-offset: 2px; }
.layout-option-text { display: flex; flex-direction: column; min-width: 0; }
.layout-option-text small { color: var(--text-2); }
.layout-preview {
  flex: 0 0 64px; height: 44px; display: flex; flex-direction: column; gap: 4px; padding: 5px;
  border: 1px solid var(--border); border-radius: var(--r-md); background: var(--surface);
}
.lp-bar { height: 6px; border-radius: 2px; background: var(--text-2); opacity: 0.35; }
.lp-bar--status { height: 10px; background: var(--ok); opacity: 1; }
.layout-preview--standard .lp-bar--status { display: none; }
.layout-preview--liste .lp-bar { border-radius: 4px; }
/* The slug is part of the URL (lower case); the global .meta-tag rule
   would otherwise set capitals ("SYSTEM"). */
.meta-tag--slug { text-transform: none; letter-spacing: normal; }

/* Design preview: the public page in its own token layer (.theme-public)
   inside a wide modal; the modal scrolls, the edit panel stays as it is. */
.design-preview { margin: 0 0 var(--sp-4); }
.design-preview-modal { inline-size: min(1200px, calc(100vw - 2 * var(--sp-4))); }
.design-preview-hint { margin: var(--sp-1) 0 0; }
.design-preview-frame {
  margin: var(--sp-3) var(--onyx-modal-padding-inline, var(--sp-5)) var(--sp-5);
  padding: 0 var(--sp-5) var(--sp-5);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  background: var(--bg);
  color: var(--text);
  min-inline-size: 0;
}
@media (max-width: 600px) {
  .design-preview-frame { margin-inline: var(--sp-3); padding: 0 var(--sp-3) var(--sp-3); }
}
</style>
