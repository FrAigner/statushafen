<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { sanitizeMailPreview } from '../../utils/mailPreview'
import { OnyxButton, OnyxInput, OnyxSelect, OnyxTextarea } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { useContentLanguages } from '../../composables/useContentLanguages'
import ListCard from '../../components/ListCard.vue'
import AppIcon from '../../components/AppIcon.vue'
import PageHeader from '../../components/PageHeader.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import PlaceholderSuggestions from '../../components/PlaceholderSuggestions.vue'
import { languageOptions } from '../../utils/notificationTemplateOptions'
import { NOTIFICATION_TEMPLATE_PLACEHOLDERS } from '../../utils/notificationTemplatePlaceholders'
import { usePlaceholderInsert, placeholderToken } from '../../composables/usePlaceholderInsert'
import { confirmDialog } from '../../composables/useConfirm'
import type { NotificationTemplate } from '../../types/api'

// Notification mail templates: editable templates for incident notifications
// per (kind, audience, language). If a combination has no entry, the backend
// sends the hard-coded default mail - "Reset" is therefore just a DELETE on the
// same quartet, after which the default applies again.
//
// Audience was added with stage 5 of multilingual support: before, the target
// group (internal/subscriber) was implicit in the language value
// (en=internal, de=subscriber). Language is now a real locale code from the
// language catalog that can be enabled per status page, instead of a
// disguised target group.

const { apiFetch } = useApi()
const { hasPerm } = useAuth()
const { activeTenantId } = useTenant()
const { pageLanguages, loadPageLanguages } = useContentLanguages()
const { t } = useI18n()

// Direction C: page header with breadcrumbs; editor and preview sit in cards with a card header.
const breadcrumbs = usePageCrumbs(() => t('notificationTemplates.title'))

// Three groups: incident kinds, maintenance window kinds, general notice
// kinds. Notices only trigger mails when the notice is created/updated with
// the "Notify subscribers" toggle - the editor here works independently anyway.
const kindOptions = computed(() => [
  { label: t('notificationTemplates.kindIncidentCreated'), value: 'incident_created' },
  { label: t('notificationTemplates.kindIncidentUpdated'), value: 'incident_updated' },
  { label: t('notificationTemplates.kindIncidentResolved'), value: 'incident_resolved' },
  { label: t('notificationTemplates.kindMaintenanceReminder'), value: 'maintenance_reminder' },
  { label: t('notificationTemplates.kindMaintenanceStarted'), value: 'maintenance_started' },
  { label: t('notificationTemplates.kindMaintenanceCompleted'), value: 'maintenance_completed' },
  { label: t('notificationTemplates.kindNoticeCreated'), value: 'notice_created' },
  { label: t('notificationTemplates.kindNoticeUpdated'), value: 'notice_updated' },
  { label: t('notificationTemplates.kindNoticeEnded'), value: 'notice_ended' },
  // Double opt-in confirmation mail for email subscriptions.
  { label: t('subscribeConfirm.templateKind'), value: 'subscription_confirm' },
  // Reports from external sources, subscribers only.
  { label: t('notificationTemplates.kindExternalIncidentCreated'), value: 'external_incident_created' },
  { label: t('notificationTemplates.kindExternalIncidentUpdated'), value: 'external_incident_updated' },
  { label: t('notificationTemplates.kindExternalIncidentResolved'), value: 'external_incident_resolved' },
  { label: t('notificationTemplates.kindExternalMaintenanceCreated'), value: 'external_maintenance_created' },
  { label: t('notificationTemplates.kindExternalMaintenanceUpdated'), value: 'external_maintenance_updated' },
  { label: t('notificationTemplates.kindExternalMaintenanceResolved'), value: 'external_maintenance_resolved' },
])

const audienceOptions = computed(() => [
  { label: t('notificationTemplates.audienceStaff'), value: 'staff' },
  { label: t('notificationTemplates.audienceSubscriber'), value: 'subscriber' },
])

// The placeholders come from the single data source in
// utils/notificationTemplatePlaceholders.ts; the click list and
// autocompletion use the same list.
const placeholders = computed(() => NOTIFICATION_TEMPLATE_PLACEHOLDERS.map(p => ({
  name: p.name,
  key: placeholderToken(p.name),
  desc: t(p.descKey),
})))

const templates = ref<NotificationTemplate[]>([])
const selectedKind = ref('incident_created')
const selectedAudience = ref('staff')
const selectedLanguage = ref('en')
const subject = ref('')
const body = ref('')
const previewSubject = ref('')
const previewBody = ref('')
const alertMsg = ref('')
const alertType = ref('info')
const isSaving = ref(false)
const isPreviewing = ref(false)
const isResetting = ref(false)

// Example values as bound strings. They used to be in the template as
// placeholder="[ALERT] {{ '{{.ComponentNames}}' }} …"; Vue does not
// interpolate static attributes, so the field showed the mustache code
// literally.
const subjectPlaceholder = '[ALERT] {{.ComponentNames}}: {{.IncidentTitle}}'
const bodyPlaceholder = '<p>{{.Message}}</p>'

// Insert placeholders by click or autocompletion.
// OnyxInput/OnyxTextarea do not expose their native element, so the view
// fetches it from the surrounding wrapper and attaches the handlers to it via
// event delegation.
const subjectField = ref<HTMLElement | null>(null)
const bodyField = ref<HTMLElement | null>(null)
const {
  suggestions, activeIndex, isOpen, listboxId, optionId, close: closeSuggestions,
  insertPlaceholder, accept, onFocus, onInput, onCaretMove, onKeydown, ariaFor,
} = usePlaceholderInsert({
  placeholders,
  fields: {
    subject: { model: subject, element: () => subjectField.value!.querySelector('input') },
    body: { model: body, element: () => bodyField.value!.querySelector('textarea') },
  },
  defaultField: 'body',
  idPrefix: 'notification-template',
})

const showAlert = (msg: string, type = 'info') => {
  alertMsg.value = msg
  alertType.value = type
}

// Falls back to languageOptions (en/de) as long as the status page has not
// configured its own language catalog yet (see the backfill/default seeding in
// database.backfillStatusPageLanguages) - identical to the fallback in
// isValidNotificationLanguage in the backend.
const availableLanguageOptions = computed(() => {
  if (!pageLanguages.value || pageLanguages.value.length === 0) return languageOptions
  return pageLanguages.value.map(l => ({ label: l.LanguageCode, value: l.LanguageCode }))
})

// With the translation switch off the page offers only its default language; keep the editor on
// an offered language (the page default first), otherwise it would edit and save a language the
// backend refuses. Not immediate: pageLanguages is shared state and may still hold the
// languages of the previously opened page until loadPageLanguages has finished.
watch(availableLanguageOptions, (options) => {
  if (options.some(o => o.value === selectedLanguage.value)) return
  // options is never empty (availableLanguageOptions falls back to de/en).
  selectedLanguage.value = pageLanguages.value.find(l => l.IsDefault)?.LanguageCode ?? options[0]!.value
})

const currentTemplate = computed(() =>
  templates.value.find(t => t.Kind === selectedKind.value && t.Audience === selectedAudience.value && t.Language === selectedLanguage.value)
)

const hasCustomTemplate = computed(() => !!currentTemplate.value)

const loadTemplates = async () => {
  if (!activeTenantId.value) return
  try {
    const [tpls] = await Promise.all([
      apiFetch<NotificationTemplate[]>(`/api/pages/${activeTenantId.value}/notification-templates`),
      loadPageLanguages(activeTenantId.value),
    ])
    templates.value = tpls
    fillEditorFromSelected()
  } catch (err) {
    showAlert(t('common.loadError') + (err as Error).message, 'error')
  }
}

// When the view refills the fields itself (selection changed, "Reset to default"), the required
// fields should be pristine again. Onyx remembers that a value was changed and showed empty fields
// red right away ("Required field"). A new key = freshly mounted fields without that state.
const editorKey = ref(0)
const fillEditorFromSelected = () => {
  editorKey.value++
  if (currentTemplate.value) {
    subject.value = currentTemplate.value.Subject
    body.value = currentTemplate.value.BodyHTML
  } else {
    subject.value = ''
    body.value = ''
  }
  previewSubject.value = ''
  previewBody.value = ''
}

watch(activeTenantId, (v) => { if (v) loadTemplates() }, { immediate: true })
// Mails about external reports only go to subscribers; an internal template
// for them would never be sent, so the audience switches over.
watch(selectedKind, (kind) => {
  if (kind.startsWith('external_')) selectedAudience.value = 'subscriber'
})
watch([selectedKind, selectedAudience, selectedLanguage], fillEditorFromSelected)

const save = async () => {
  if (!subject.value || !body.value) {
    showAlert(t('notificationTemplates.requiredFields'), 'warning')
    return
  }
  isSaving.value = true
  try {
    await apiFetch(
      `/api/pages/${activeTenantId.value}/notification-templates/${selectedKind.value}/${selectedAudience.value}/${selectedLanguage.value}`,
      { method: 'PUT', body: JSON.stringify({ Subject: subject.value, BodyHTML: body.value }) }
    )
    showAlert(t('notificationTemplates.saved'), 'success')
    await loadTemplates()
  } catch (err) {
    showAlert(t('common.saveError') + (err as Error).message, 'error')
  } finally {
    isSaving.value = false
  }
}

const resetToDefault = async () => {
  if (!hasCustomTemplate.value) {
    showAlert(t('notificationTemplates.noCustomToReset'), 'info')
    return
  }
  if (!(await confirmDialog({ message: t('notificationTemplates.confirmReset'), confirmLabel: t('confirm.reset') }))) return
  isResetting.value = true
  try {
    await apiFetch(
      `/api/pages/${activeTenantId.value}/notification-templates/${selectedKind.value}/${selectedAudience.value}/${selectedLanguage.value}`,
      { method: 'DELETE' }
    )
    showAlert(t('notificationTemplates.resetDone'), 'success')
    await loadTemplates()
  } catch (err) {
    showAlert(t('notificationTemplates.resetError') + (err as Error).message, 'error')
  } finally {
    isResetting.value = false
  }
}

// After "Preview" the view jumps to the preview (2026-10-29): it sits below
// the form, and after the click one only saw the success message at the top
// and had to scroll oneself. Focus goes to the heading so that screen reader
// and keyboard users land there too (preventScroll: scrollIntoView does the
// scrolling, otherwise the view jumps twice).
const previewHeading = ref<HTMLElement | null>(null)
const scrollToPreview = async () => {
  await nextTick()
  const el = previewHeading.value
  if (!el) return
  el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  el.focus({ preventScroll: true })
}

const preview = async () => {
  if (!subject.value || !body.value) {
    showAlert(t('notificationTemplates.requiredFields'), 'warning')
    return
  }
  isPreviewing.value = true
  try {
    const resp: { subject: string, bodyHTML: string } = await apiFetch(
      `/api/pages/${activeTenantId.value}/notification-templates/${selectedKind.value}/${selectedAudience.value}/${selectedLanguage.value}/preview`,
      { method: 'POST', body: JSON.stringify({ Subject: subject.value, BodyHTML: body.value }) }
    )
    previewSubject.value = resp.subject
    previewBody.value = resp.bodyHTML
    showAlert(t('notificationTemplates.previewUpdated'), 'success')
    await scrollToPreview()
  } catch (err) {
    showAlert(t('notificationTemplates.previewError') + (err as Error).message, 'error')
  } finally {
    isPreviewing.value = false
  }
}

// Save = update, reset = delete, preview = read (every reader may see it).
const canEdit = computed(() => hasPerm('notification_templates:update'))
const canDelete = computed(() => hasPerm('notification_templates:delete'))

// previewBody comes from the backend preview endpoint and contains the freely
// editable HTML of the template. Before v-html rendering it runs through the
// allowlist for mail previews (utils/mailPreview.ts): no script, but also no
// forms, input fields, images or overlay styles that could fake a login form
// to other admins.
const sanitizedPreviewBody = computed(() => sanitizeMailPreview(previewBody.value))
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('notificationTemplates.title')" :subtitle="t('notificationTemplates.description')" :breadcrumbs="breadcrumbs" />

    <ListCard :title="t('notificationTemplates.editorTitle')" padded overflow-visible class="fade-in">
      <template #tools>
        <span class="tag-pill" :class="{ 'tag-pill--accent': hasCustomTemplate }">{{ hasCustomTemplate ? t('notificationTemplates.badgeCustom') : t('notificationTemplates.badgeDefault') }}</span>
      </template>
      <div class="form-grid">
        <OnyxSelect v-model="selectedKind" :options="kindOptions" :label="t('notificationTemplates.fieldKind')" :list-label="t('notificationTemplates.fieldKind')" />
        <OnyxSelect v-model="selectedAudience" :options="audienceOptions" :label="t('notificationTemplates.fieldAudience')" :list-label="t('notificationTemplates.fieldAudience')" />
        <OnyxSelect v-model="selectedLanguage" :options="availableLanguageOptions" :label="t('notificationTemplates.fieldLanguage')" :list-label="t('notificationTemplates.fieldLanguage')" />
      </div>

      <p v-if="!hasCustomTemplate" class="panel-lead">
        <em>{{ t('notificationTemplates.noCustomTemplate') }}</em>
      </p>
      <p v-else class="panel-lead">
        <em>{{ t('notificationTemplates.customTemplateActive', { date: new Date(currentTemplate!.UpdatedAt!).toLocaleString() }) }}</em>
      </p>

      <!-- novalidate, because the view checks the required fields itself
           (notificationTemplates.requiredFields). Without novalidate Chrome
           showed its native "Please fill out this field." message for the
           empty required fields as a hover tooltip or a validation bubble
           below the body field, i.e. over "Available placeholders". -->
      <form :key="editorKey" class="edit-form" novalidate data-self-validates @submit.prevent="save">
        <div
          ref="subjectField"
          class="form-group placeholder-field"
          @focusin="onFocus('subject')"
          @focusout="closeSuggestions"
          @input="onInput('subject', $event)"
          @keydown="onKeydown('subject', $event)"
          @keyup="onCaretMove('subject', $event)"
          @click="onCaretMove('subject', $event)"
        >
          <OnyxInput
            v-model="subject"
            v-bind="ariaFor('subject', true)"
            required
            :placeholder="subjectPlaceholder"
            :label="t('notificationTemplates.fieldSubject')"
            :disabled="!canEdit"
          />
          <PlaceholderSuggestions
            :id="listboxId('subject')"
            :label="t('notificationTemplates.suggestionsLabel')"
            :items="suggestions"
            :open="isOpen('subject')"
            :active-index="activeIndex"
            :option-id="(index) => optionId('subject', index)"
            @select="(item) => accept('subject', item)"
          />
        </div>
        <div
          ref="bodyField"
          class="form-group placeholder-field"
          @focusin="onFocus('body')"
          @focusout="closeSuggestions"
          @input="onInput('body', $event)"
          @keydown="onKeydown('body', $event)"
          @keyup="onCaretMove('body', $event)"
          @click="onCaretMove('body', $event)"
        >
          <OnyxTextarea
            v-model="body"
            v-bind="ariaFor('body', false)"
            required
            :placeholder="bodyPlaceholder"
            :label="t('notificationTemplates.fieldBody')"
            :disabled="!canEdit"
          />
          <PlaceholderSuggestions
            :id="listboxId('body')"
            :label="t('notificationTemplates.suggestionsLabel')"
            :items="suggestions"
            :open="isOpen('body')"
            :active-index="activeIndex"
            :option-id="(index) => optionId('body', index)"
            @select="(item) => accept('body', item)"
          />
        </div>

        <details class="placeholder-details">
          <summary>{{ t('notificationTemplates.placeholdersSummary') }}</summary>
          <p class="text-muted placeholder-hint">{{ t('notificationTemplates.placeholdersHint') }}</p>
          <ul class="placeholder-list">
            <li v-for="p in placeholders" :key="p.key">
              <button
                type="button"
                class="placeholder-insert"
                :aria-label="t('notificationTemplates.insertPlaceholder', { key: p.key })"
                :disabled="!canEdit"
                @click="insertPlaceholder(p)"
              ><code>{{ p.key }}</code></button>
              <span> - {{ p.desc }}</span>
            </li>
          </ul>
        </details>

        <div class="form-actions">
          <OnyxButton
            v-if="canDelete"
            :label="isResetting ? t('notificationTemplates.resetting') : t('notificationTemplates.resetButton')"
            mode="outline"
            type="button"
            @click="resetToDefault"
            :disabled="isResetting || !hasCustomTemplate"
          />
          <!-- The preview only renders and is a read action in the backend
               (notification_templates:read, POST .../preview). -->
          <OnyxButton
            :label="isPreviewing ? t('notificationTemplates.previewing') : t('notificationTemplates.previewButton')"
            mode="outline"
            type="button"
            @click="preview"
            :disabled="isPreviewing"
          />
          <OnyxButton
            v-if="canEdit"
            :label="isSaving ? t('common.saving') : t('common.save')"
            type="submit"
            :disabled="isSaving"
          />
        </div>
      </form>
    </ListCard>

    <section v-if="previewSubject || previewBody" class="comp-card fade-in admin-section-gap" aria-labelledby="notif-preview-title">
      <div class="comp-card__head">
        <h3 id="notif-preview-title" ref="previewHeading" tabindex="-1" class="comp-card__title preview-heading"><AppIcon name="eye" :size="16" /> {{ t('notificationTemplates.previewHeading') }}</h3>
      </div>
      <div class="list-card__body">
      <div class="form-group">
        <strong>{{ t('notificationTemplates.subjectLabel') }}</strong>
        <div class="preview-box preview-box--mono">
          {{ previewSubject }}
        </div>
      </div>
      <div class="form-group">
        <strong>{{ t('notificationTemplates.bodyLabel') }}</strong>
        <div class="preview-box" v-html="sanitizedPreviewBody"></div>
      </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
/* Anchor for the suggestion list directly below the field. */
.placeholder-field {
  position: relative;
}

.placeholder-details {
  margin: 8px 0 16px;
}

.placeholder-details summary {
  cursor: pointer;
}

.placeholder-hint {
  margin: 8px 0 0;
}

.placeholder-list {
  margin: 8px 0 0 16px;
}

.placeholder-list li {
  margin: 2px 0;
}

.placeholder-insert {
  padding: 1px 6px;
  font: inherit;
  color: var(--text);
  background: var(--surface-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.placeholder-insert code {
  font-family: var(--font-mono);
}

.placeholder-insert:hover:not(:disabled) {
  background: var(--primary-soft);
  border-color: var(--primary);
}

.placeholder-insert:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 1px;
}

.placeholder-insert:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 16px;
}

/* Smartphone (480px+): 2 columns */
@media (min-width: 480px) {
  .form-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* Tablet (768px+): 2 columns */
@media (min-width: 768px) {
  .form-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* Desktop (1200px+): 3 columns */
@media (min-width: 1200px) {
  .form-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}
</style>
