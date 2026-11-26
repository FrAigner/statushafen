<script setup lang="ts">
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
import { canSendTestMessage, testMessagePayload } from '../../utils/testMessageTarget'
import { vHelp } from '../../directives/vHelp'
import type { Subscriber } from '../../types/api'
import type { ComponentTreeNode, FlatComponent } from '../../utils/componentTree'
import type { TestMessageForm } from '../../utils/testMessageTarget'

const { apiFetch } = useApi()
// Permissions of this tab per action. Without update an existing entry opens
// read-only (formReadOnly); without delete the row menu entry is gone.
const { canCreate, canUpdate, canDelete } = useResourcePerms('subscribers')
const formReadOnly = computed(() => currentEditId.value !== null && !canUpdate.value)
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()

const items         = ref<Subscriber[]>([])
const loading       = ref(false)
const loadFailed    = ref(false)
const components    = ref<FlatComponent[]>([])
// Languages of the page for the subscriber's mail language.
const pageLanguages = ref<ReturnType<typeof fromAdminPageLanguages>>([])
const isEditing     = ref(false)
const currentEditId = ref<number | null>(null)
const formData      = ref<Partial<Subscriber>>({})
const alertMsg      = ref('')
const alertType     = ref('warning')
const isSaving      = ref(false)
const deletingId    = ref<number | null>(null)
const testingUrl    = ref<string | null | undefined>(null)
// Test message from the open form: result is shown inline at the form.
const sendingTestMessage = ref(false)
const testMessageResult  = ref<{ type: 'success' | 'error', text: string } | null>(null)
const query         = ref('')
const editName      = ref<string | undefined>('')
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

// SubscribedComponentIDs stays a comma-separated string field for the API
// (empty = all), but the form edits it via a multi-select by component name
// instead of free-text IDs - the same conversion approach as ComponentIDs in
// AdminIncidents.vue, only with a string instead of an array as wire format.
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
      apiFetch<Subscriber[]>(`/api/pages/${activeTenantId.value}/subscribers`),
      apiFetch<ComponentTreeNode[]>(`/api/pages/${activeTenantId.value}/components`),
      // without permission for the language list, or on error, only the language choice is dropped.
      apiFetch(`/api/pages/${activeTenantId.value}/languages`).catch(() => []),
    ])
    items.value = subs
    components.value = flattenComponentTree(comps) // including subcomponents
    pageLanguages.value = fromAdminPageLanguages(langs)
  } catch (err) {
    loadFailed.value = true
    showAlert(t('common.loadError') + (err as Error).message, 'error')
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
// remove the previous step's message before every new action.
const clearAlert = () => { alertMsg.value = '' }
const showAlert = (msg: string, type = 'warning') => {
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

const openEdit = (item: Subscriber) => {
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
  // Required contact field (email or webhook URL): error at the field instead of a server error.
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
    showAlert(t('common.saveError') + (err as Error).message, 'error')
  } finally {
    isSaving.value = false
  }
}

const deleteItem = async (id: number, name: string) => {
  if (!(await confirmDialog({ message: t('subscribers.confirmDelete', { name }) }))) return
  clearAlert()
  deletingId.value = id
  try {
    await apiFetch(`/api/pages/${activeTenantId.value}/subscribers/${id}`, { method: 'DELETE' })
    showAlert(t('adminFeedback.subscriberDeleted'), 'success')
    await loadData()
  } catch (err) {
    showAlert(t('common.deleteError') + (err as Error).message, 'error')
  } finally {
    deletingId.value = null
  }
}

const testWebhook = async (url: string | undefined) => {
  clearAlert()
  testingUrl.value = url
  try {
    await apiFetch('/api/subscribe/test-webhook', {
      method: 'POST',
      body: JSON.stringify({ webhook_url: url })
    })
    showAlert(t('subscribers.testSuccess'), 'success')
  } catch (err) {
    showAlert(t('subscribers.testError') + (err as Error).message, 'error')
  } finally {
    testingUrl.value = null
  }
}

const testMessageReady = computed(() => canSendTestMessage(formData.value as TestMessageForm))

// A new target makes the last result meaningless.
watch(() => [formData.value.Channel, formData.value.Email, formData.value.WebhookURL], () => {
  testMessageResult.value = null
})

// Sends to the unsaved target in the admin UI language (locale), not in the
// subscriber's mail language. The button is disabled without a valid target.
const sendTestMessage = async () => {
  sendingTestMessage.value = true
  testMessageResult.value = null
  try {
    await apiFetch(`/api/pages/${activeTenantId.value}/subscribers/test`, {
      method: 'POST',
      body: JSON.stringify(testMessagePayload(formData.value as TestMessageForm, locale.value)),
    })
    testMessageResult.value = { type: 'success', text: t('subscribers.testMessageSent') }
  } catch (err) {
    testMessageResult.value = { type: 'error', text: t('subscribers.testMessageFailed') + (err as Error).message }
  } finally {
    sendingTestMessage.value = false
  }
}

const formTitle = computed(() => currentEditId.value
  ? t('subscribers.editTitle', { name: editName.value })
  : t('subscribers.createTitle'))

// Direction C: page header with a count, search, row menu.
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
const menuItems = (s: Subscriber) => [
  { key: 'edit', label: canUpdate.value ? t('common.edit') : t('common.view'), icon: canUpdate.value ? 'pencil' : 'eye' },
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === s.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === s.ID }] : []),
]
const onMenuSelect = (s: Subscriber, key: string) => {
  if (key === 'edit') openEdit(s)
  else deleteItem(s.ID, s.Email || t('subscribers.channelGoogleChat'))
}

const channelLabel = (ch: string) => channelOptions.value.find(o => o.value === ch)?.label ?? ch

// show component names instead of raw IDs in the table as well.
const componentLabels = (idsCsv: string) => componentLabelsFromCsv(idsCsv, components.value, t('subscribers.allComponents'))
</script>

<template>
  <div>

    <!-- Alert -->
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" data-help-exempt="Meldung schließen, selbsterklärend" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('subscribers.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template #actions>
        <OnyxButton
          v-if="canCreate"
          v-help="'subscribers.newSubscriber'"
          :label="t('subscribers.newButton')"
          @click="openNew"
        />
      </template>
    </PageHeader>

    <!-- Direction C: table card on the left, form as a side card on the right. -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard class="split__main" :title="t('subscribers.cardTitle')">
        <template #tools>
          <SearchBox v-model="query" v-help="'subscribers.search'" :label="t('subscribers.searchLabel')" />
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
                  <!-- unconfirmed subscription (double opt-in) -->
                  <StatusBadge v-if="s.Status === 'pending'" status="degraded" :label="t('subscribeConfirm.adminPendingBadge')" class="inline-badge badge-pending" :title="t('subscribeConfirm.adminPendingTitle')" />
                </template>
                <div v-else class="cell-contact">
                  <span class="cell-truncate" :title="s.WebhookURL">{{ s.WebhookURL }}</span>
                  <!-- The test is open to readers too, hence not in the row menu (only with subscribers:update). -->
                  <button v-help="'subscribers.testWebhook'" class="action-btn" @click="testWebhook(s.WebhookURL)" :disabled="testingUrl === s.WebhookURL">
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
                <RowMenu v-help="'subscribers.rowActions'" :label="t('subscribers.menuLabel', { name: s.Email || s.WebhookURL })" :items="menuItems(s)" @select="key => onMenuSelect(s, key)" />
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

          <FormField v-slot="f" :label="t('subscribers.fieldChannel')" required help="subscribers.channel">
            <select :id="f.id" v-model="formData.Channel" :required="f.required" @change="fieldError = ''">
              <option v-for="o in channelOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </FormField>

          <FormField v-if="formData.Channel === 'email'" v-slot="f" :label="t('subscribers.fieldEmail')" required help="subscribers.email" :error="fieldError || null">
            <input :id="f.id" v-model="formData.Email" type="email" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('subscribers.emailPlaceholder')" @input="fieldError = ''">
          </FormField>

          <FormField v-else v-slot="f" :label="t('subscribers.fieldWebhookUrl')" required help="subscribers.webhookUrl" :error="fieldError || null">
            <input :id="f.id" v-model="formData.WebhookURL" type="url" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('subscribers.webhookPlaceholder')" @input="fieldError = ''">
          </FormField>

          <div class="form-group test-message">
            <button
              type="button"
              v-help="'subscribers.testMessage'"
              class="action-btn"
              data-test="send-test-message"
              aria-describedby="subscriber-test-message-hint"
              :disabled="!testMessageReady || sendingTestMessage"
              @click="sendTestMessage"
            >
              {{ sendingTestMessage ? t('subscribers.testMessageSending') : t('subscribers.testMessageButton') }}
            </button>
            <p id="subscriber-test-message-hint" class="form-field__message">{{ t('subscribers.testMessageHint') }}</p>
            <p
              v-if="testMessageResult"
              data-test="test-message-result"
              role="status"
              :class="`alert alert-${testMessageResult.type}`"
            >{{ testMessageResult.text }}</p>
          </div>

          <FormField v-if="showLanguageChoice" v-slot="f" :label="t('subscribers.fieldLanguage')" help="subscribers.language" :hint="t('subscribers.fieldLanguageHint')">
            <select :id="f.id" v-model="formData.Language" :aria-describedby="f.describedby">
              <option v-for="o in languageOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </FormField>

          <div class="form-group">
            <OnyxSelect
              v-model="selectedComponentIds"
              v-help="'subscribers.components'"
              :options="componentOptions(components, selectedComponentIds)"
              multiple
              :label="t('subscribers.fieldComponents')"
              :list-label="t('subscribers.fieldComponents')"
            />
          </div>

          </fieldset>
          <div class="form-actions">
            <OnyxButton v-help="'subscribers.cancel'" :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton v-if="!formReadOnly" v-help="'subscribers.save'" :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>

  </div>
</template>
