<script setup>
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

// Notification-Mail-Templates: per (Kind, Audience, Language)
// editierbare Vorlagen für Incident-Benachrichtigungen. Fehlt für eine
// Kombination ein Eintrag, versendet das Backend die hartkodierte
// Default-Mail - "Reset" ist deshalb nur ein DELETE auf demselben
// Quartett, danach greift wieder der Default.
//
// Audience kam mit Stage 5 der Mehrsprachigkeit dazu: vorher
// steckte die Zielgruppe (intern/Subscriber) implizit im Language-Wert
// (en=intern, de=Subscriber). Language ist jetzt ein echter, pro Statusseite
// aktivierbarer Locale-Code aus dem Sprachkatalog statt einer
// verkappten Zielgruppen-Angabe.

const { apiFetch } = useApi()
const { hasPerm } = useAuth()
const { activeTenantId } = useTenant()
const { pageLanguages, loadPageLanguages } = useContentLanguages()
const { t } = useI18n()

// Richtung C: Seitenkopf mit Brotkrumen; Editor und Vorschau stehen in Karten mit Kartenkopf.
const breadcrumbs = usePageCrumbs(() => t('notificationTemplates.title'))

// Drei Gruppen: Incident-Kinds, Wartungsfenster-Kinds,
// General-Notice-Kinds. Notices lösen nur dann Mails aus, wenn
// die Notice mit dem "Subscriber benachrichtigen"-Toggle angelegt/
// aktualisiert wird - der Editor hier funktioniert trotzdem unabhängig.
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
  // Bestätigungsmail des Double-Opt-in für E-Mail-Abos.
  { label: t('subscribeConfirm.templateKind'), value: 'subscription_confirm' },
  // Meldungen externer Quellen, nur für Abonnenten.
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

// Die Platzhalter kommen aus der einen Datenquelle in
// utils/notificationTemplatePlaceholders.js; Klickliste und
// Autovervollständigung nutzen dieselbe Liste.
const placeholders = computed(() => NOTIFICATION_TEMPLATE_PLACEHOLDERS.map(p => ({
  name: p.name,
  key: placeholderToken(p.name),
  desc: t(p.descKey),
})))

const templates = ref([])
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

// Beispielwerte als gebundene Strings. Vorher standen sie als
// placeholder="[ALERT] {{ '{{.ComponentNames}}' }} …" im Template; Vue
// interpoliert in statischen Attributen nicht, im Feld stand deshalb der
// Mustache-Code wörtlich.
const subjectPlaceholder = '[ALERT] {{.ComponentNames}}: {{.IncidentTitle}}'
const bodyPlaceholder = '<p>{{.Message}}</p>'

// Platzhalter per Klick oder Autovervollständigung einfügen.
// OnyxInput/OnyxTextarea geben ihr natives Element nicht heraus, deshalb
// holt die View es aus dem umschließenden Wrapper und hängt die Handler per
// Event-Delegation an diesen.
const subjectField = ref(null)
const bodyField = ref(null)
const {
  suggestions, activeIndex, isOpen, listboxId, optionId, close: closeSuggestions,
  insertPlaceholder, accept, onFocus, onInput, onCaretMove, onKeydown, ariaFor,
} = usePlaceholderInsert({
  placeholders,
  fields: {
    subject: { model: subject, element: () => subjectField.value.querySelector('input') },
    body: { model: body, element: () => bodyField.value.querySelector('textarea') },
  },
  defaultField: 'body',
  idPrefix: 'notification-template',
})

const showAlert = (msg, type = 'info') => {
  alertMsg.value = msg
  alertType.value = type
}

// Fällt auf languageOptions (en/de) zurück, solange die Statusseite noch
// keinen eigenen Sprachkatalog konfiguriert hat (siehe Backfill/
// Default-Seeding in database.backfillStatusPageLanguages) - identisch zum
// Fallback in isValidNotificationLanguage im Backend.
const availableLanguageOptions = computed(() => {
  if (!pageLanguages.value || pageLanguages.value.length === 0) return languageOptions
  return pageLanguages.value.map(l => ({ label: l.LanguageCode, value: l.LanguageCode }))
})

const currentTemplate = computed(() =>
  templates.value.find(t => t.Kind === selectedKind.value && t.Audience === selectedAudience.value && t.Language === selectedLanguage.value)
)

const hasCustomTemplate = computed(() => !!currentTemplate.value)

const loadTemplates = async () => {
  if (!activeTenantId.value) return
  try {
    const [tpls] = await Promise.all([
      apiFetch(`/api/pages/${activeTenantId.value}/notification-templates`),
      loadPageLanguages(activeTenantId.value),
    ])
    templates.value = tpls
    fillEditorFromSelected()
  } catch (err) {
    showAlert(t('common.loadError') + err.message, 'error')
  }
}

// Füllt die View die Felder selbst neu (Auswahl gewechselt, „Auf Default zurücksetzen“), sollen die
// Pflichtfelder wieder unberührt sein. Onyx merkt sich, dass ein Wert geändert wurde, und zeigte leere
// Felder sofort rot („Pflichtfeld“). Neuer Schlüssel = frisch gemountete Felder ohne diesen Zustand.
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
// Mails zu externen Meldungen gehen nur an Abonnenten; ein internes
// Template dafür würde nie versendet, deshalb springt die Zielgruppe um.
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
    showAlert(t('common.saveError') + err.message, 'error')
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
    showAlert(t('notificationTemplates.resetError') + err.message, 'error')
  } finally {
    isResetting.value = false
  }
}

// Nach „Vorschau“ springt die Ansicht zur Vorschau (2026-10-29): Sie steht
// unter dem Formular, man sah nach dem Klick nur die Erfolgsmeldung oben und
// musste selbst scrollen. Der Fokus geht auf die Überschrift, damit auch
// Screenreader- und Tastaturnutzer dort landen (preventScroll: das Scrollen
// übernimmt scrollIntoView, sonst springt die Ansicht doppelt).
const previewHeading = ref(null)
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
    const resp = await apiFetch(
      `/api/pages/${activeTenantId.value}/notification-templates/${selectedKind.value}/${selectedAudience.value}/${selectedLanguage.value}/preview`,
      { method: 'POST', body: JSON.stringify({ Subject: subject.value, BodyHTML: body.value }) }
    )
    previewSubject.value = resp.subject
    previewBody.value = resp.bodyHTML
    showAlert(t('notificationTemplates.previewUpdated'), 'success')
    await scrollToPreview()
  } catch (err) {
    showAlert(t('notificationTemplates.previewError') + err.message, 'error')
  } finally {
    isPreviewing.value = false
  }
}

// Speichern = Ändern, Zurücksetzen = Löschen, Vorschau = Lesen (jeder Leser darf sie sehen).
const canEdit = computed(() => hasPerm('notification_templates:update'))
const canDelete = computed(() => hasPerm('notification_templates:delete'))

// previewBody kommt vom Backend-Preview-Endpoint und enthält das frei
// editierbare HTML der Vorlage. Vor dem v-html-Rendering läuft es durch die
// Allowlist für Mail-Vorschauen (utils/mailPreview.js): kein Skript, aber auch
// keine Formulare, Eingabefelder, Bilder oder Overlay-Styles, die anderen Admins
// eine Anmeldemaske vortäuschen könnten.
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
        <em>{{ t('notificationTemplates.customTemplateActive', { date: new Date(currentTemplate.UpdatedAt).toLocaleString() }) }}</em>
      </p>

      <!-- novalidate, weil die View die Pflichtfelder selbst
           prüft (notificationTemplates.requiredFields). Ohne novalidate zeigte
           Chrome für die leeren required-Felder seine native Meldung „Fülle
           dieses Feld aus.“ als Hover-Tooltip bzw. als Validierungsblase
           unter dem Body-Feld, also über „Verfügbare Platzhalter“. -->
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
          <!-- Die Vorschau rendert nur und ist im Backend eine Leseaktion
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
/* Anker für die Vorschlagsliste direkt unter dem Feld. */
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

/* Smartphone (480px+): 2 spalten */
@media (min-width: 480px) {
  .form-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* Tablet (768px+): 2 spalten */
@media (min-width: 768px) {
  .form-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* Desktop (1200px+): 3 spalten */
@media (min-width: 1200px) {
  .form-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}
</style>
