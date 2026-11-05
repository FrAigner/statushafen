<script setup>
import { ref, computed, watch } from 'vue'
import { OnyxButton, OnyxSelect } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useResourcePerms } from '../../composables/useResourcePerms'
import { useTenant } from '../../composables/useTenant'
import AppIcon from '../../components/AppIcon.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import { flattenComponentTree, componentOptions } from '../../utils/componentTree'
import ListState from '../../components/ListState.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormPanel from '../../components/FormPanel.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import RowMenu from '../../components/RowMenu.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useFormPanel } from '../../composables/useFormPanel'
import { formatRelative } from '../../utils/relativeTime'
import FormField from '../../components/FormField.vue'
import EmptyState from '../../components/EmptyState.vue'
import { idsFromCsv, csvFromIds, componentLabelsFromCsv } from '../../utils/subscribedComponents'
import { fromAdminPageLanguages, hasLanguageChoice, subscriberLanguageOptions, selectableSubscriberLanguage } from '../../utils/subscriberLanguage'
import { confirmDialog } from '../../composables/useConfirm'

const { apiFetch } = useApi()
// Rechte dieses Tabs je Aktion. Ohne Ändern öffnet sich ein bestehender
// Eintrag schreibgeschützt (formReadOnly), ohne Löschen entfällt der Eintrag im Zeilenmenü.
const { canCreate, canUpdate, canDelete } = useResourcePerms('subscribers')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()

const items         = ref([])
const loading       = ref(false)
const loadFailed    = ref(false)
const components    = ref([])
// Sprachen der Seite für die Mailsprache des Abonnenten.
const pageLanguages = ref([])
const isEditing     = ref(false)
const currentEditId = ref(null)
const formData      = ref({})
const alertMsg      = ref('')
const alertType     = ref('warning')
const isSaving      = ref(false)
const deletingId    = ref(null)
const testingUrl    = ref(null)
const query         = ref('')
const editName      = ref('')
const fieldError    = ref('')
const { formEl, focusForm, focusInvalid } = useFormPanel()

const showLanguageChoice = computed(() => hasLanguageChoice(pageLanguages.value))
const languageOptions = computed(() =>
  subscriberLanguageOptions(pageLanguages.value, (name) => t('subscribers.languageDefault', { name }))
)

const channelOptions = computed(() => [
  { label: t('subscribers.channelEmail'),      value: 'email' },
  { label: t('subscribers.channelGoogleChat'), value: 'google_chat' },
])

// SubscribedComponentIDs bleibt für die API ein kommasepariertes
// String-Feld (leer = alle), aber im Formular wird es per Multi-Select nach
// Komponentenname statt Freitext-IDs bearbeitet - derselbe Umwandlungs-
// Ansatz wie ComponentIDs bei AdminIncidents.vue, nur mit String statt Array
// als Wire-Format.
const selectedComponentIds = computed({
  get: () => idsFromCsv(formData.value.SubscribedComponentIDs),
  set: (ids) => { formData.value.SubscribedComponentIDs = csvFromIds(ids) },
})

/* ── Data loading ── */
const loadData = async () => {
  loading.value = true
  loadFailed.value = false
  try {
    const [subs, comps, langs] = await Promise.all([
      apiFetch(`/api/pages/${activeTenantId.value}/subscribers`),
      apiFetch(`/api/pages/${activeTenantId.value}/components`),
      // ohne Recht auf die Sprachliste oder bei Fehler entfällt nur die Sprachwahl.
      apiFetch(`/api/pages/${activeTenantId.value}/languages`).catch(() => []),
    ])
    items.value = subs
    components.value = flattenComponentTree(comps) // inkl. Unterkomponenten
    pageLanguages.value = fromAdminPageLanguages(langs)
  } catch (err) {
    loadFailed.value = true
    showAlert(t('common.loadError') + err.message, 'error')
  } finally {
    loading.value = false
  }
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

const openNew = () => {
  formData.value  = { Channel: 'email', Language: '' }
  currentEditId.value = null
  editName.value = ''
  fieldError.value = ''
  isEditing.value = true
  focusForm()
}

const openEdit = (item) => {
  formData.value  = { ...item }
  if (showLanguageChoice.value) formData.value.Language = selectableSubscriberLanguage(item.Language ?? '', pageLanguages.value)
  currentEditId.value = item.ID
  editName.value = item.Channel === 'email' ? item.Email : item.WebhookURL
  fieldError.value = ''
  isEditing.value = true
  focusForm()
}

const cancelEdit = () => {
  isEditing.value = false
  formData.value  = {}
  currentEditId.value = null
  fieldError.value = ''
}

const saveItem = async () => {
  // Pflichtfeld Kontakt (E-Mail bzw. Webhook-URL): Fehler am Feld statt eines Serverfehlers.
  const isEmail = formData.value.Channel === 'email'
  if (!((isEmail ? formData.value.Email : formData.value.WebhookURL) || '').trim()) {
    fieldError.value = t(isEmail ? 'subscribers.emailRequired' : 'subscribers.webhookRequired')
    await focusInvalid()
    return
  }
  const isNew = currentEditId.value === null
  const url   = isNew
    ? `/api/pages/${activeTenantId.value}/subscribers`
    : `/api/pages/${activeTenantId.value}/subscribers/${currentEditId.value}`
  clearAlert()
  isSaving.value = true
  try {
    await apiFetch(url, { method: isNew ? 'POST' : 'PUT', body: JSON.stringify(formData.value) })
    showAlert(isNew ? t('subscribers.created') : t('common.changesSaved'), 'success')
    cancelEdit()
    await loadData()
  } catch (err) {
    showAlert(t('common.saveError') + err.message, 'error')
  } finally {
    isSaving.value = false
  }
}

const deleteItem = async (id, name) => {
  if (!(await confirmDialog({ message: t('subscribers.confirmDelete', { name }) }))) return
  clearAlert()
  deletingId.value = id
  try {
    await apiFetch(`/api/pages/${activeTenantId.value}/subscribers/${id}`, { method: 'DELETE' })
    showAlert(t('adminFeedback.subscriberDeleted'), 'success')
    await loadData()
  } catch (err) {
    showAlert(t('common.deleteError') + err.message, 'error')
  } finally {
    deletingId.value = null
  }
}

const testWebhook = async (url) => {
  clearAlert()
  testingUrl.value = url
  try {
    await apiFetch('/api/subscribe/test-webhook', {
      method: 'POST',
      body: JSON.stringify({ webhook_url: url })
    })
    showAlert(t('subscribers.testSuccess'), 'success')
  } catch (err) {
    showAlert(t('subscribers.testError') + err.message, 'error')
  } finally {
    testingUrl.value = null
  }
}

const formTitle = computed(() => currentEditId.value
  ? t('subscribers.editTitle', { name: editName.value })
  : t('subscribers.createTitle'))

// Richtung C: Seitenkopf mit Zählung, Suche, Zeilenmenü.
const breadcrumbs = usePageCrumbs(() => t('subscribers.title'))
const subtitle = computed(() => {
  const pending = items.value.filter(s => s.Status === 'pending').length
  return t('subscribers.subtitle', {
    subs: t('subscribers.countSubscribers', { n: items.value.length }, items.value.length),
    pending: t('subscribers.countPending', { n: pending }, pending),
  })
})
const searching = computed(() => query.value.trim() !== '')
const rows = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return items.value.filter(s => `${s.Email || ''} ${s.WebhookURL || ''} ${s.CreatedBy?.Email || ''}`.toLowerCase().includes(needle))
})
const menuItems = (s) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === s.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === s.ID }] : []),
]
const onMenuSelect = (s, key) => {
  if (key === 'edit') openEdit(s)
  else deleteItem(s.ID, s.Email || t('subscribers.channelGoogleChat'))
}

const channelLabel = (ch) => channelOptions.value.find(o => o.value === ch)?.label ?? ch

// auch in der Tabelle Komponentennamen statt roher IDs anzeigen.
const componentLabels = (idsCsv) => componentLabelsFromCsv(idsCsv, components.value, t('subscribers.allComponents'))
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('subscribers.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template #actions>
        <OnyxButton
          v-if="canCreate"
          :label="t('subscribers.newButton')"
          @click="openNew"
        />
      </template>
    </PageHeader>

    <!-- Richtung C: Tabellenkarte links, Formular als Seitenkarte rechts. -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard class="split__main" :title="t('subscribers.cardTitle')">
        <template #tools>
          <SearchBox v-model="query" :label="t('subscribers.searchLabel')" />
        </template>
        <p v-if="searching" class="sr-only" role="status">{{ t('subscribers.searchResult', { n: rows.length }, rows.length) }}</p>

        <table v-if="rows.length" class="data-table data-table--cards">
          <thead>
            <tr>
              <th scope="col">{{ t('subscribers.tableChannel') }}</th>
              <th scope="col">{{ t('subscribers.tableContact') }}</th>
              <th scope="col">{{ t('subscribers.tableComponents') }}</th>
              <th scope="col">{{ t('subscribers.tableCreatedBy') }}</th>
              <th scope="col">{{ t('subscribers.tableAdded') }}</th>
              <th scope="col" class="actions-col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in rows" :key="s.ID">
              <td :data-label="t('subscribers.tableChannel')"><span class="tag-pill">{{ channelLabel(s.Channel) }}</span></td>
              <td :data-label="t('subscribers.tableContact')">
                <template v-if="s.Channel === 'email'">
                  <strong>{{ s.Email }}</strong>
                  <!-- unbestätigtes Abo (Double-Opt-in) -->
                  <StatusBadge v-if="s.Status === 'pending'" status="degraded" :label="t('subscribeConfirm.adminPendingBadge')" class="inline-badge badge-pending" :title="t('subscribeConfirm.adminPendingTitle')" />
                </template>
                <div v-else class="cell-contact">
                  <span class="cell-truncate" :title="s.WebhookURL">{{ s.WebhookURL }}</span>
                  <!-- Der Test steht auch Lesenden offen, daher nicht im Zeilenmenü (nur mit subscribers:update). -->
                  <button class="action-btn" @click="testWebhook(s.WebhookURL)" :disabled="testingUrl === s.WebhookURL">
                    {{ testingUrl === s.WebhookURL ? t('subscribers.testSending') : t('subscribers.testSend') }}
                  </button>
                </div>
              </td>
              <td :data-label="t('subscribers.tableComponents')" class="text-muted">{{ componentLabels(s.SubscribedComponentIDs) }}</td>
              <td :data-label="t('subscribers.tableCreatedBy')" class="text-muted" :title="s.CreatedBy ? t('subscribers.createdByOwnTitle') : t('subscribers.createdByNoneTitle')">
                {{ s.CreatedBy?.Email || '—' }}
              </td>
              <td :data-label="t('subscribers.tableAdded')" class="cell-changed">
                <RelativeTime :value="s.CreatedAt" />
              </td>
              <td class="actions-col">
                <RowMenu :label="t('subscribers.menuLabel', { name: s.Email || s.WebhookURL })" :items="menuItems(s)" @select="key => onMenuSelect(s, key)" />
              </td>
            </tr>
          </tbody>
        </table>
        <EmptyState v-else-if="searching && items.length" :title="t('subscribers.noResults')" :text="t('subscribers.noResultsText')" />
        <ListState v-else :loading="loading" :error="loadFailed" :count="items.length" :empty-text="t('subscribers.empty')" />
      </ListCard>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="saveItem">
          <p v-if="formReadOnly" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
          <fieldset class="form-lock" :disabled="formReadOnly">

          <FormField v-slot="f" :label="t('subscribers.fieldChannel')" required>
            <select :id="f.id" v-model="formData.Channel" :required="f.required" @change="fieldError = ''">
              <option v-for="o in channelOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </FormField>

          <FormField v-if="formData.Channel === 'email'" v-slot="f" :label="t('subscribers.fieldEmail')" required :error="fieldError || null">
            <input :id="f.id" v-model="formData.Email" type="email" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('subscribers.emailPlaceholder')" @input="fieldError = ''">
          </FormField>

          <FormField v-else v-slot="f" :label="t('subscribers.fieldWebhookUrl')" required :error="fieldError || null">
            <input :id="f.id" v-model="formData.WebhookURL" type="url" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('subscribers.webhookPlaceholder')" @input="fieldError = ''">
          </FormField>

          <FormField v-if="showLanguageChoice" v-slot="f" :label="t('subscribers.fieldLanguage')" :hint="t('subscribers.fieldLanguageHint')">
            <select :id="f.id" v-model="formData.Language" :aria-describedby="f.describedby">
              <option v-for="o in languageOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </FormField>

          <div class="form-group">
            <OnyxSelect
              v-model="selectedComponentIds"
              :options="componentOptions(components, selectedComponentIds)"
              multiple
              :label="t('subscribers.fieldComponents')"
              :list-label="t('subscribers.fieldComponents')"
            />
          </div>

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
