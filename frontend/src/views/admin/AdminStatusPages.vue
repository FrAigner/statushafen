<script setup>
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { OnyxButton } from 'sit-onyx'
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
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { formatRelative } from '../../utils/relativeTime'
import { serverInputErrorText } from '../../utils/inputRules'
import { primaryBaseDomain } from '../../utils/appDomains'
// "Global Admin" ist keine Seitenrolle (GlobalOnly aus /api/roles).
import { pageRoleOptions, userAdminErrorText } from '../../utils/userAdmin'
import { confirmDialog } from '../../composables/useConfirm'

const { apiFetch } = useApi()
const { isGlobalAdmin } = useAuth()
const { t, locale } = useI18n()

const pages = ref([])
const users = ref([])
const roles = ref([])
const isEditing = ref(false)
const showUsers = ref(null) // StatusPage ID für User-Management
const currentEditItem = ref(null)
const formData = ref({})

// wählbare Seiten-Layouts (Werte wie models.StatusPage.PageLayout).
const pageLayoutOptions = [
  { value: 'standard', label: 'statusPages.pageLayoutStandard', desc: 'statusPages.pageLayoutStandardDesc' },
  { value: 'liste', label: 'statusPages.pageLayoutListe', desc: 'statusPages.pageLayoutListeDesc' },
]

// Slug-Auto-Generierung aus dem Namen: so lange der Nutzer den
// Slug bei einer neuen Statusseite noch nicht selbst angefasst hat, wird er
// live aus dem Namen abgeleitet - identische Logik zu slugify() im Backend
// (backend/handlers/statuspage.go), nur als Vorschau; die endgültige,
// kollisionssichere Generierung passiert serverseitig, falls das Feld beim
// Speichern trotzdem leer bleibt.
const slugTouched = ref(false)
function slugify(name) {
  const umlauts = { 'ä': 'ae', 'ö': 'oe', 'ü': 'ue', 'ß': 'ss' }
  return (name || '')
    .toLowerCase()
    .replace(/[äöüß]/g, ch => umlauts[ch])
    .split(/[^a-z0-9]+/)
    .filter(Boolean)
    .join('-')
}
watch(() => formData.value.Name, (name) => {
  if (currentEditItem.value === 'new' && !slugTouched.value) {
    formData.value.Slug = slugify(name)
  }
})

const userAssignData = ref({ user_id: '', role_id: '' })
const pageUsers = ref([])
// offene Einladungen der Seite (per E-Mail eingeladen, noch nicht angenommen).
const pageInvitations = ref([])
const revokingInvitationId = ref(null)
const alertMsg = ref('')
// Erfolgsmeldungen tragen die Art „success“ (blenden sich selbst aus), alles andere bleibt Warnung;
// clearAlert entfernt die Meldung des vorigen Schritts.
const successMsg = ref('')
const alertType = computed(() => (alertMsg.value && alertMsg.value === successMsg.value ? 'success' : 'warning'))
const clearAlert = () => { alertMsg.value = '' }
const showSuccess = (msg) => {
  successMsg.value = msg
  alertMsg.value = msg
}
const isSaving = ref(false)
const deletingId = ref(null)
const isAssigning = ref(false)
const removingUserId = ref(null)

// Monitoring-Webhook-Token: der Klartext-Token wird vom Backend
// nur direkt nach dem Generieren zurückgegeben - hier nur transient im
// State gehalten, nie in formData/pages gespeichert oder erneut nachgeladen.
const webhookToken = ref('')
const isGeneratingToken = ref(false)
const isRevokingToken = ref(false)
const tokenCopied = ref(false)

// Custom Domain: läuft über eigene Endpunkte statt über das
// generische Speichern-Formular, damit ein Domain-Wechsel immer zuverlässig
// den Verifikationsstatus zurücksetzt (siehe Backend-Kommentar in
// statuspage.go). customDomain* hält den zuletzt vom Backend bestätigten
// Stand, customDomainInput das noch nicht gespeicherte Eingabefeld.
const customDomainInput = ref('')
const customDomainStatus = ref('')
const cnameTarget = ref('')
const customDomainMessage = ref('')
const isSavingDomain = ref(false)
const isVerifyingDomain = ref(false)
const isRemovingDomain = ref(false)
// TXT-Owner-Verifizierung: txtRecordValue kommt nur direkt aus der
// Set-/Verify-Response (CustomDomainVerificationToken ist json:"-" im Backend,
// analog zum WebhookToken), customDomainVerifiedAt kommt dagegen normal aus den
// Page-Daten (nicht versteckt) und übersteht daher auch ein erneutes Öffnen des
// Formulars ohne erneuten Save/Verify-Aufruf.
const txtRecordValue = ref('')
const customDomainVerifiedAt = ref(null)
// Letzte (fehlgeschlagene) DNS-Prüfung: Zeitpunkt und Grund-Code.
const customDomainCheckedAt = ref(null)
const customDomainCheckError = ref('')
const txtCopied = ref(false)

// Stufe 4d: Tabellenspalten und Feldhinweise. Die Hinweise sind
// reiner Text (FormField kennt keine <strong>-Auszeichnung im Hinweis).
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
// Ohne konfigurierte Basisdomain (VITE_APP_BASE_DOMAINS) gibt es keine Subdomain:
// die Statusseite ist dann unter /status/<slug> erreichbar, und der Standard-Absender
// kommt vom Server.
const slugHint = computed(() => {
  const slug = formData.value.Slug || '…'
  const base = primaryBaseDomain()
  return base
    ? `${t('statusPages.slugHintPrefix')} ${slug}.${base} ${t('statusPages.slugHintSuffix')}`
    : t('statusPages.slugHintPath', { slug })
})
const edgeTargetFallback = computed(() => `edge.${primaryBaseDomain() || 'example.com'}`)

// Richtung C: Seitenkopf, Suche, Zeilenmenü und Formularfehler am Feld.
const breadcrumbs = usePageCrumbs(computed(() => t('adminNav.items.pages')), { global: true })
const subtitle = computed(() => t('statusPages.subtitle', {
  pages: t('statusPages.countPages', { n: pages.value.length }, pages.value.length),
  priv: t('statusPages.countPrivate', { n: privateCount.value }, privateCount.value),
}))
const privateCount = computed(() => pages.value.filter(p => p.IsPrivate).length)
const query = ref('')
const searching = computed(() => query.value.trim() !== '')
const searchText = p => `${p.Name} ${p.Slug} ${p.Description || ''} ${p.CustomDomain || ''}`.toLowerCase()
const rows = computed(() => {
  const q = query.value.trim().toLowerCase()
  return q ? pages.value.filter(p => searchText(p).includes(q)) : pages.value
})
const formTitle = computed(() => {
  if (currentEditItem.value === 'new') return t('statusPages.createTitle')
  return t('statusPages.editTitle', { name: formData.value.Name })
})
const formErrors = ref({ name: '', slug: '' })
// Der Slug der Systemseite ist fest (Backend: system_page_slug_locked).
const isSystemPageEdit = computed(() => currentEditItem.value !== 'new' && !!formData.value.IsSystem)
const slugFieldHint = computed(() => {
  if (currentEditItem.value === 'new') return slugHint.value
  return isSystemPageEdit.value ? t('statusPages.systemSlugLocked') : null
})
const formEl = ref(null)

const menuItems = (p) => [
  { key: 'open', label: t('statusPages.openAction'), icon: 'globe' },
  { key: 'users', label: t('statusPages.usersAction'), icon: 'users' },
  ...(isGlobalAdmin.value ? [
    { key: 'edit', label: t('common.edit'), icon: 'pencil' },
    // Die Systemseite lässt sich nicht löschen (Backend: 409).
    ...(p.IsSystem ? [] : [{ key: 'delete', label: deletingId.value === p.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === p.ID }]),
  ] : []),
]
const onMenuSelect = (p, key) => {
  if (key === 'open') openPublicPage(p)
  else if (key === 'users') openUserManagement(p)
  else if (key === 'edit') openEdit(p)
  else deleteItem(p.ID, p.Name)
}

const loadData = async () => {
  try {
    const [p, u, r] = await Promise.all([
      apiFetch('/api/pages'),
      apiFetch('/api/users'),
      apiFetch('/api/roles'),
    ])
    pages.value = p
    users.value = u
    roles.value = r
  } catch (err) {
    alertMsg.value = t('statusPages.loadError') + err.message
  }
}

const loadPageUsers = async (pageId) => {
  // H6: Beide Listen unabhängig laden; scheitert nur die
  // Einladungsliste, bleibt die Mitgliederliste sichtbar. Der Zustand wird erst
  // nach den awaits gesetzt und nur, wenn noch dieselbe Seite offen ist (sonst
  // überschriebe die späte Antwort einer zuvor geöffneten Seite die aktuelle).
  const [members, invitations] = await Promise.allSettled([
    apiFetch(`/api/admin/pages/${pageId}/users`),
    apiFetch(`/api/admin/pages/${pageId}/invitations`),
  ])
  if (showUsers.value?.ID !== pageId) return
  if (members.status === 'rejected') throw members.reason
  pageUsers.value = members.value
  pageInvitations.value = invitations.status === 'fulfilled' ? invitations.value : []
}

onMounted(loadData)

const openEdit = (item) => {
  formData.value = item === 'new'
    ? {
      // Stryker disable next-line StringLiteral: Der Name-Watcher überschreibt den Slug beim Öffnen sofort (undefined -> ''), der Startwert ist nicht beobachtbar
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
      // Stryker disable next-line StringLiteral: Der Slug-Watcher leitet die Adresse beim Öffnen sofort neu ab, der Startwert ist nicht beobachtbar
      NotificationFromAddress: ''
    }
    : JSON.parse(JSON.stringify(item))
  currentEditItem.value = item === 'new' ? 'new' : item.ID
  slugTouched.value = false
  webhookToken.value = ''
  tokenCopied.value = false
  customDomainInput.value = item === 'new' ? '' : (item.CustomDomain || '')
  customDomainStatus.value = item === 'new' ? '' : (item.CustomDomainStatus || '')
  customDomainMessage.value = ''
  txtRecordValue.value = ''
  customDomainVerifiedAt.value = item === 'new' ? null : (item.CustomDomainVerifiedAt || null)
  customDomainCheckedAt.value = item === 'new' ? null : (item.CustomDomainCheckedAt || null)
  customDomainCheckError.value = item === 'new' ? '' : (item.CustomDomainCheckError || '')
  formErrors.value = { name: '', slug: '' }
  showUsers.value = null
  isEditing.value = true
  focusForm()
}

// Formular öffnen: Fokus auf das Namensfeld; unter 860 px liegt das Formular
// unter der Liste und wird in den Sichtbereich geholt.
const focusForm = async () => {
  await nextTick()
  formEl.value?.closest('aside')?.scrollIntoView?.({ block: 'nearest' })
  formEl.value?.querySelector('input')?.focus({ preventScroll: true })
}

const cancelEdit = () => {
  isEditing.value = false
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
}

const customDomainStatusLabel = (status) => ({
  pending:  t('statusPages.domainPending'),
  check_failed: t('statusPages.domainCheckFailed'),
  verified: t('statusPages.domainVerified'),
  active:   t('statusPages.domainActive'),
}[status] ?? t('statusPages.domainNone'))

// Domain-Status auf die Statusstufe der StatusBadge: ausstehend = Warnung,
// bestätigt = Hinweis (Wartungsform), aktiv = in Ordnung; Unbekanntes bleibt neutral.
// Grund der letzten fehlgeschlagenen Prüfung (Backend-Code -> Text); unbekannte
// Codes zeigen den allgemeinen Text.
const customDomainCheckReason = computed(() => ({
  txt_not_found: t('statusPages.checkReasonTxtNotFound'),
  dns_lookup_failed: t('statusPages.checkReasonLookupFailed'),
}[customDomainCheckError.value] ?? t('statusPages.checkReasonUnknown')))

const DOMAIN_LEVEL = { pending: 'degraded', check_failed: 'major', verified: 'maintenance', active: 'operational' }
const domainStatusLevel = (status) => DOMAIN_LEVEL[status] ?? null

const saveCustomDomain = async () => {
  isSavingDomain.value = true
  customDomainMessage.value = ''
  txtRecordValue.value = ''
  try {
    const resp = await apiFetch(`/api/admin/pages/${currentEditItem.value}/custom-domain`, {
      method: 'PUT',
      body: JSON.stringify({ domain: customDomainInput.value })
    })
    customDomainStatus.value = resp.custom_domain_status
    cnameTarget.value = resp.cname_target
    txtRecordValue.value = resp.txt_record_value || ''
    await loadData()
  } catch (err) {
    alertMsg.value = t('statusPages.domainSaveError') + err.message
  } finally {
    isSavingDomain.value = false
  }
}

const verifyCustomDomain = async () => {
  isVerifyingDomain.value = true
  customDomainMessage.value = ''
  txtRecordValue.value = ''
  try {
    const resp = await apiFetch(`/api/admin/pages/${currentEditItem.value}/custom-domain/verify`, { method: 'POST' })
    customDomainStatus.value = resp.custom_domain_status
    customDomainCheckedAt.value = resp.custom_domain_checked_at || null
    customDomainCheckError.value = resp.custom_domain_check_error || ''
    customDomainMessage.value = resp.message
    cnameTarget.value = resp.cname_target
    txtRecordValue.value = resp.txt_record_value || ''
    if (resp.verified_at) {
      customDomainVerifiedAt.value = resp.verified_at
    }
    await loadData()
  } catch (err) {
    alertMsg.value = t('statusPages.domainVerifyError') + err.message
  } finally {
    isVerifyingDomain.value = false
  }
}

const removeCustomDomain = async () => {
  // Ziel vor dem Dialog festhalten (die Auswahl kann sich währenddessen ändern).
  const pageId = currentEditItem.value
  if (!(await confirmDialog({ message: t('statusPages.confirmRemoveDomain'), confirmLabel: t('confirm.remove') }))) return
  isRemovingDomain.value = true
  try {
    await apiFetch(`/api/admin/pages/${pageId}/custom-domain`, { method: 'DELETE' })
    customDomainInput.value = ''
    customDomainStatus.value = ''
    customDomainMessage.value = ''
    await loadData()
  } catch (err) {
    alertMsg.value = t('statusPages.domainRemoveError') + err.message
  } finally {
    isRemovingDomain.value = false
  }
}

const generateWebhookToken = async () => {
  // Ziel vor dem Dialog festhalten (die Auswahl kann sich währenddessen ändern).
  const pageId = currentEditItem.value
  if (!(await confirmDialog({ message: t('statusPages.confirmGenerateToken'), confirmLabel: t('confirm.generate') }))) return
  isGeneratingToken.value = true
  tokenCopied.value = false
  try {
    const resp = await apiFetch(`/api/admin/pages/${pageId}/webhook-token`, { method: 'POST' })
    webhookToken.value = resp.token
  } catch (err) {
    alertMsg.value = t('statusPages.tokenGenerateError') + err.message
  } finally {
    isGeneratingToken.value = false
  }
}

const revokeWebhookToken = async () => {
  // Ziel vor dem Dialog festhalten (die Auswahl kann sich währenddessen ändern).
  const pageId = currentEditItem.value
  if (!(await confirmDialog({ message: t('statusPages.confirmRevokeToken'), confirmLabel: t('confirm.revoke') }))) return
  isRevokingToken.value = true
  try {
    await apiFetch(`/api/admin/pages/${pageId}/webhook-token`, { method: 'DELETE' })
    webhookToken.value = ''
    tokenCopied.value = false
  } catch (err) {
    alertMsg.value = t('statusPages.tokenRevokeError') + err.message
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
    alertMsg.value = t('statusPages.copyError') + err.message
  }
}

const copyTxtRecord = async () => {
  try {
    await navigator.clipboard.writeText(txtRecordValue.value)
    txtCopied.value = true
    setTimeout(() => { txtCopied.value = false }, 2000)
  } catch (err) {
    alertMsg.value = t('statusPages.copyError') + err.message
  }
}

// Eingabefehler des Backends (400 mit code) erscheinen am Feld.
const onSaveError = async (err) => {
  const text = serverInputErrorText(err, t)
  const field = { name_required: 'name', slug_reserved: 'slug', slug_taken: 'slug' }[err.data?.code]
  if (!text || !field) {
    alertMsg.value = t('statusPages.saveError') + err.message
    return
  }
  formErrors.value = { ...formErrors.value, [field]: text }
  await nextTick()
  formEl.value?.querySelector('[aria-invalid="true"]')?.focus()
}

const saveAction = useSaveAction({ apiFetch, isSaving, reload: loadData, onStart: clearAlert, onError: onSaveError })
const deleteAction = useDeleteAction({ apiFetch, deletingId, reload: loadData, onStart: clearAlert, onError: (err) => { alertMsg.value = t('statusPages.deleteError') + err.message } })

const saveItem = async () => {
  const isNew = currentEditItem.value === 'new'
  // Pflichtfelder: Fehler am Feld statt eines Serverfehlers, Fokus aufs erste Fehlerfeld.
  formErrors.value = {
    name: (formData.value.Name || '').trim() ? '' : t('statusPages.nameRequired'),
    slug: !isNew && !(formData.value.Slug || '').trim() ? t('statusPages.slugRequired') : '',
  }
  if (formErrors.value.name || formErrors.value.slug) {
    await nextTick()
    formEl.value?.querySelector('[aria-invalid="true"]')?.focus()
    return
  }
  const endpoint = `/api/admin/pages${isNew ? '' : '/' + currentEditItem.value}`
  const payload = { ...formData.value }
  payload.DefaultRoleID = payload.DefaultRoleID ? Number.parseInt(payload.DefaultRoleID) : null

  await saveAction(endpoint, isNew ? 'POST' : 'PUT', payload, cancelEdit)
}

const deleteItem = (id, name) =>
  deleteAction(`/api/admin/pages/${id}`, id, t('statusPages.confirmDelete', { name }))

const openUserManagement = async (page) => {
  cancelEdit()
  showUsers.value = page
  await loadPageUsers(page.ID)
}

const openPublicPage = (page) => {
  window.open('/status/' + page.Slug, '_blank')
}

const assignUser = async () => {
  if (!userAssignData.value.user_id || !userAssignData.value.role_id) return
  isAssigning.value = true
  try {
    await apiFetch(`/api/admin/pages/${showUsers.value.ID}/users`, {
      method: 'POST',
      body: JSON.stringify({
        user_id: Number.parseInt(userAssignData.value.user_id),
        role_id: Number.parseInt(userAssignData.value.role_id),
      })
    })
    userAssignData.value = { user_id: '', role_id: '' }
    await loadPageUsers(showUsers.value.ID)
  } catch (err) {
    alertMsg.value = t('statusPages.assignError') + userAdminErrorText(err, t)
  } finally {
    isAssigning.value = false
  }
}

// Einladung zurückziehen; der Link in der Mail wird damit ungültig.
const revokeInvitation = async (invitation) => {
  const pageId = showUsers.value?.ID
  if (!(await confirmDialog({ message: t('statusPages.confirmRevokeInvitation', { email: invitation.Email }), confirmLabel: t('statusPages.revokeInvitation') }))) return
  clearAlert()
  revokingInvitationId.value = invitation.ID
  try {
    await apiFetch(`/api/admin/pages/${pageId}/invitations/${invitation.ID}`, { method: 'DELETE' })
    showSuccess(t('statusPages.invitationRevoked'))
    await loadPageUsers(pageId)
  } catch (err) {
    alertMsg.value = t('statusPages.revokeInvitationError') + err.message
  } finally {
    revokingInvitationId.value = null
  }
}

const removeUser = async (assignment) => {
  // Löschen fragt sonst überall nach, die Zuweisung nicht.
  const userId = assignment?.User?.ID
  const name = assignment?.User?.Email || '–'
  const pageId = showUsers.value?.ID
  if (!(await confirmDialog({ message: t('adminFeedback.confirmRemoveUser', { name }), confirmLabel: t('confirm.remove') }))) return
  clearAlert()
  removingUserId.value = userId
  try {
    await apiFetch(`/api/admin/pages/${pageId}/users/${userId}`, { method: 'DELETE' })
    showSuccess(t('adminFeedback.userRemoved'))
    await loadPageUsers(pageId)
  } catch (err) {
    alertMsg.value = t('statusPages.removeUserError') + err.message
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

    <!-- Richtung C: Liste links, Formular bzw. Benutzerverwaltung als
         Seitenkarte rechts (unter 860 px darunter). -->
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

      <!-- Formular -->
      <FormPanel v-if="isEditing" class="form-panel--scroll" :title="formTitle" @close="cancelEdit">
        <!-- Name/Slug prüft saveItem selbst (Meldung am Feld), der Submit-Wächter
             soll nicht dazwischenfunken. -->
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

          <!-- Seiten-Layout mit kleiner Vorschau je Variante -->
          <fieldset class="form-group layout-picker">
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

        <!-- Monitoring-Webhook-Token: nur für bereits gespeicherte Seiten, -->
        <!-- eine "neue" Seite hat noch keine ID, gegen die ein Token generiert werden könnte. -->
        <section v-if="currentEditItem !== 'new'" class="adm-section">
          <h4 class="adm-subhead">{{ t('statusPages.webhookHeading') }}</h4>
          <p class="section-text">
            {{ t('statusPages.webhookDescPrefix') }}
            <code>POST /api/webhooks/components/&lt;component_id&gt;/status</code> {{ t('statusPages.webhookDescMid') }}
            <code>X-Webhook-Token</code> {{ t('statusPages.webhookDescSuffix') }}
          </p>

          <FormField v-if="webhookToken" v-slot="f" :label="t('statusPages.newTokenLabel')" field-id="webhook-token-display">
            <div class="adm-row adm-row--nowrap">
              <input :id="f.id" :value="webhookToken" class="form-input token-input" readonly @focus="$event.target.select()" />
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

        <!-- Custom Domain -->
        <section v-if="currentEditItem !== 'new'" class="adm-section">
          <h4 class="adm-subhead">{{ t('statusPages.customDomainHeading') }}</h4>
          <p class="section-text">
            {{ t('statusPages.customDomainDescPrefix') }} <code>status.example.org</code> {{ t('statusPages.customDomainDescMid') }}
            <code v-if="cnameTarget">{{ cnameTarget }}</code><code v-else>{{ edgeTargetFallback }}</code> {{ t('statusPages.customDomainDescSuffix') }}
          </p>

          <FormField v-slot="f" :label="t('statusPages.fieldDomain')" field-id="custom-domain-input">
            <div class="adm-row adm-row--nowrap">
              <input :id="f.id" v-model="customDomainInput" class="form-input" :placeholder="t('statusPages.domainPlaceholder')" />
              <button type="button" class="action-btn" @click="saveCustomDomain" :disabled="isSavingDomain">
                {{ isSavingDomain ? t('common.saving') : t('common.save') }}
              </button>
            </div>
          </FormField>

          <p v-if="customDomainStatus" class="status-line">
            <StatusBadge :status="domainStatusLevel(customDomainStatus)" :label="customDomainStatusLabel(customDomainStatus)" />
          </p>

          <!-- TXT-Verifizierung: Zuerst TXT-Eintrag bestätigen (DNS-Besitz), dann CNAME -->
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

          <!-- Letzter Prüffehler: bei verified/active ändert er den Status nicht, die Domain bleibt in Betrieb -->
          <p v-if="customDomainCheckError && customDomainCheckedAt" class="section-text section-text--small" data-test="domain-check-failed">
            {{ t('statusPages.checkFailedAt', { date: new Date(customDomainCheckedAt).toLocaleString(), reason: customDomainCheckReason }) }}
            <template v-if="['verified', 'active'].includes(customDomainStatus)"> {{ t('statusPages.checkFailedKept') }}</template>
          </p>

          <p v-if="customDomainMessage" class="section-text">{{ customDomainMessage }}</p>

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

      <!-- Benutzerverwaltung -->
      <FormPanel v-else-if="showUsers" class="form-panel--scroll" :title="t('statusPages.permissionsTitle', { name: showUsers.Name })" :show-required-hint="false" @close="showUsers = null">
        <!-- Benutzer zuweisen -->
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

        <!-- Aktuelle Benutzer -->
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

        <!-- offene Einladungen -->
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
/* Hex-Feld nimmt den Rest neben der Farbfläche und darf schrumpfen. */
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
/* Das lange Domain-Badge ("Ausstehend – DNS noch nicht geprüft") darf in der
   Karte (390 px) umbrechen statt rechts abgeschnitten zu werden. */
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
/* Der Slug ist Teil der URL (klein geschrieben); die globale
   .meta-tag-Regel setzt sonst Großbuchstaben ("SYSTEM"). */
.meta-tag--slug { text-transform: none; letter-spacing: normal; }
</style>
