<script setup>
/*
  Incident-Detail: Verlauf (Timeline) + Post-Mortem.
  Route:   { path: 'incidents/:pageId/:id', component: AdminIncidentDetail }
  Backend: GET /api/pages/:page_id/incidents/:id   (handlers/incident_detail.go)

  Erwartet die Route-Parameter pageId und id. Verlinkbar aus AdminIncidents.vue:
      <router-link :to="`/admin/incidents/${activeTenantId}/${i.ID}`">…</router-link>

  Redesign Stufe 4b: PageHeader mit Brotkrumen (Seitentitel = h2
  wie in allen Admin-Views, die h1 trägt die Topbar), Verlauf als Zeitleiste
  (<ol>) mit StatusBadge, FormField für die Formulare, keine Inline-Styles.

  Richtung C: Brotkrumen „Seitenname › Vorfälle › #ID“ und ein
  Untertitel mit der relativen Meldezeit wie auf den Listenseiten; der Inhalt
  nutzt die Breite links bündig statt zentriert.
*/
import { ref, computed, nextTick, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { OnyxButton } from 'sit-onyx'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import AppIcon from '../../components/AppIcon.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormField from '../../components/FormField.vue'
import RichTextEditor from '../../components/RichTextEditor.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import { toIntlLocale } from '../../utils/datetime'
import { formatRelative } from '../../utils/relativeTime'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { incidentBadge, actionBadge } from '../../utils/badgeStatus'
import { useFormTranslations } from '../../composables/useFormTranslations'
import FormLanguageSwitch from '../../components/FormLanguageSwitch.vue'
import FormLanguageDone from '../../components/FormLanguageDone.vue'

const route = useRoute()
const { apiFetch } = useApi()
const { hasPerm } = useAuth()
const { t, locale } = useI18n()

const incident = ref(null)
const loading  = ref(true)
const errorMsg = ref('')

const statusOptions = computed(() => ['investigating', 'identified', 'monitoring', 'resolved']
  .map(value => ({ label: incidentBadge(value, t).label, value })))
const actionStateOptions = computed(() => ['open', 'in_progress', 'done']
  .map(value => ({ label: actionBadge(value, t).label, value })))
const fmtDate = (d) => d ? new Date(d).toLocaleString(toIntlLocale(locale.value), { dateStyle: 'medium', timeStyle: 'short' }) : '—'
const fmtDay  = (d) => d ? new Date(d).toLocaleDateString(toIntlLocale(locale.value), { dateStyle: 'long' }) : '—'

const title = computed(() => incident.value?.Title ?? t('incidentDetail.pageTitle'))
const breadcrumbs = usePageCrumbs(() => `#${incident.value?.ID ?? route.params.id}`, {
  extra: () => [{ label: t('incidentDetail.breadcrumbList'), to: '/admin/incidents' }],
})
const subtitle = computed(() => incident.value?.CreatedAt
  ? t('incidentDetail.subtitle', { when: formatRelative(incident.value.CreatedAt, locale.value) })
  : null)

// keepView: nach dem Speichern im Formular die Seite nicht durch den Ladezustand austauschen,
// sonst springt sie nach oben und die Rückmeldung läge außerhalb des Bildes.
const load = async ({ keepView = false } = {}) => {
  if (!keepView) loading.value = true
  try {
    const { pageId, id } = route.params
    incident.value = await apiFetch(`/api/pages/${pageId}/incidents/${id}`)
    resetUpdateForm()
    await trans.load(pageId)
    resetPmForm()
  } catch (err) {
    errorMsg.value = t('incidentDetail.loadError') + err.message
  } finally {
    loading.value = false
  }
  // Link „Update posten“ aus der Liste (#update): Formular ins Bild, Status fokussiert.
  // Erst nach dem Laden, vorher ist das Formular noch nicht gerendert.
  if (route.hash === '#update' && incident.value) {
    await nextTick()
    const form = document.getElementById('update-form')
    form?.scrollIntoView?.({ block: 'center' })
    form?.querySelector('select')?.focus()
  }
}
onMounted(load)

const pm = computed(() => incident.value?.post_mortem || null)
const updates = computed(() => {
  // Neueste zuerst für die Timeline-Ansicht.
  return [...(incident.value?.updates || [])].reverse()
})

/* ── Update hinzufügen ── */
const updateForm = ref({ status: '', message: '' })
const updateSaving = ref(false)
const updateAlert = ref('')

const resetUpdateForm = () => {
  updateForm.value = { status: incident.value?.Status || 'investigating', message: '' }
  trans.reset()
}

/* ── Übersetzungen der Updates ──
   Ein Update hat nur eine Nachricht (kein Titel). Sprachumschalter im Formular
   „Update posten“ und, für schon geposteter Einträge, im Editor „Übersetzungen“
   je Zeitleisten-Eintrag. Recht wie das Ändern des Vorfalls (Wartung: maintenance). */
const canTranslate = computed(() => hasPerm(`${incident.value?.IsMaintenance ? 'maintenance' : 'incidents'}:update`))
const updatesBase = () => `/api/pages/${route.params.pageId}/incidents/${route.params.id}/updates`
const transUrls = {
  list: (updateId) => `${updatesBase()}/${updateId}/translations`,
  save: (updateId, code) => `${updatesBase()}/${updateId}/translations/${code}`,
}
const transOptions = { apiFetch, urls: transUrls, contentKey: 'message', titleRequired: false, canEdit: canTranslate }
const trans = useFormTranslations({ ...transOptions, formData: updateForm })
const { contentModel: updateMessage } = trans

const editForm = ref({ message: '' })
const editTrans = useFormTranslations({ ...transOptions, formData: editForm })
const { contentModel: editMessage, onDefaultLang: editOnDefaultLang } = editTrans
const editUpdateId = ref(null)
const editSaving = ref(false)
const editAlert = ref('')

const openTranslations = async (u) => {
  editUpdateId.value = u.id
  editAlert.value = ''
  editForm.value = { message: u.message }
  await editTrans.load(route.params.pageId)
  await editTrans.loadFor(u.id)
}
const closeTranslations = () => { editUpdateId.value = null; editTrans.reset() }
const saveTranslations = async () => {
  editSaving.value = true
  editAlert.value = ''
  try {
    await editTrans.saveAll(editUpdateId.value)
    closeTranslations()
  } catch (err) {
    editAlert.value = t('incidentDetail.translationsSaveError') + err.message
  } finally {
    editSaving.value = false
  }
}

const submitUpdate = async () => {
  const { pageId, id } = route.params
  updateSaving.value = true
  updateAlert.value = ''
  try {
    const created = await apiFetch(`/api/pages/${pageId}/incidents/${id}/updates`, {
      method: 'POST',
      body: JSON.stringify({ status: updateForm.value.status, message: updateForm.value.message })
    })
    // Das Update steht ab hier; scheitert nur eine Übersetzung, bleibt es bestehen und
    // der Hinweis nennt den Weg, sie nachzuholen.
    let translationError = ''
    if (trans.showSwitcher.value) {
      try { await trans.saveAll(created.id) } catch (err) { translationError = err.message }
    }
    await load()
    if (translationError) updateAlert.value = t('incidentDetail.updateTranslationError') + translationError
  } catch (err) {
    updateAlert.value = t('incidentDetail.updateError') + err.message
  } finally {
    updateSaving.value = false
  }
}

/* ── Post-Mortem bearbeiten ── */
const pmForm = ref({ summary: '', root_cause: '', impact: '', resolution: '', detection: '', actions: [] })
const pmSaving = ref(false)
const pmAlert = ref('')
const pmNotice = ref('')

const resetPmForm = () => {
  const src = pm.value
  pmForm.value = {
    summary: src?.summary || '',
    root_cause: src?.root_cause || '',
    impact: src?.impact || '',
    resolution: src?.resolution || '',
    detection: src?.detection || '',
    actions: (src?.actions || []).map(a => ({ text: a.text, owner: a.owner, state: a.state })),
  }
}

const addActionRow = () => {
  pmForm.value.actions.push({ text: '', owner: '', state: 'open' })
}
const removeActionRow = (idx) => {
  pmForm.value.actions.splice(idx, 1)
}

const submitPostMortem = async (publish) => {
  const { pageId, id } = route.params
  pmSaving.value = true
  pmAlert.value = ''
  pmNotice.value = ''
  try {
    await apiFetch(`/api/pages/${pageId}/incidents/${id}/postmortem`, {
      method: 'PUT',
      body: JSON.stringify({ ...pmForm.value, publish })
    })
    await load({ keepView: true })
    pmNotice.value = t(publish ? 'adminFeedback.postMortemPublished' : 'adminFeedback.postMortemDraftSaved')
  } catch (err) {
    pmAlert.value = t('common.saveError') + err.message
  } finally {
    pmSaving.value = false
  }
}

// Felder des Post-Mortem-Formulars: Schlüssel, Beschriftung, Platzhalter.
const pmFields = computed(() => [
  { key: 'root_cause', label: t('incidentDetail.rootCause'), placeholder: t('incidentDetail.rootCausePlaceholder') },
  { key: 'impact', label: t('incidentDetail.impact'), placeholder: t('incidentDetail.impactPlaceholder') },
  { key: 'resolution', label: t('incidentDetail.resolution'), placeholder: t('incidentDetail.resolutionPlaceholder') },
  { key: 'detection', label: t('incidentDetail.detection'), placeholder: t('incidentDetail.detectionPlaceholder') },
])
</script>

<template>
  <div class="wrap">
    <PageHeader :level="2" :title="title" :subtitle="subtitle" :breadcrumbs="breadcrumbs" />

    <div v-if="errorMsg" class="alert alert-error">{{ errorMsg }}</div>
    <p v-else-if="loading" class="loading" role="status" aria-busy="true">{{ t('common.loading') }}</p>

    <template v-else-if="incident">
      <!-- Kopf -->
      <section class="ent-card head" :aria-label="title">
        <StatusBadge size="md" v-bind="incidentBadge(incident.Status, t)" />
        <dl class="head-meta">
          <div><dt class="m-k">{{ t('incidentDetail.metaComponents') }}</dt><dd class="m-v">{{ (incident.Components || []).map(c => c.Name).join(', ') || '—' }}</dd></div>
          <div><dt class="m-k">{{ t('incidentDetail.metaTags') }}</dt><dd class="m-v">{{ (incident.Tags || []).map(tag => tag.Name).join(', ') || '—' }}</dd></div>
          <div><dt class="m-k">{{ t('incidentDetail.metaReported') }}</dt><dd class="m-v mono">{{ fmtDate(incident.CreatedAt) }}</dd></div>
        </dl>
      </section>

      <!-- Verlauf -->
      <section class="ent-card body">
        <h3 class="section-title">{{ t('incidentDetail.historyHeading') }}</h3>
        <p v-if="updates.length === 0" class="loading">{{ t('incidentDetail.noUpdates') }}</p>
        <ol v-else class="timeline">
          <li v-for="u in updates" :key="u.id" class="tl-row">
            <div class="tl-head">
              <StatusBadge :status="incidentBadge(u.status, t).status" :label="incidentBadge(u.status, t).label" />
              <time class="tl-time mono" :datetime="u.created_at">{{ fmtDate(u.created_at) }}</time>
            </div>
            <p class="tl-msg">{{ u.message }}</p>
            <div v-if="u.author_email" class="tl-author">— {{ u.author_email }}</div>
            <div v-if="trans.showSwitcher.value && editUpdateId !== u.id" class="tl-actions">
              <OnyxButton :label="t('incidentDetail.translationsButton')" mode="plain" type="button" data-test="update-translations-open" @click="openTranslations(u)" />
            </div>
            <div v-if="editUpdateId === u.id" class="tl-trans" data-test="update-translations">
              <div v-if="editAlert" class="alert alert-error">{{ editAlert }}</div>
              <FormLanguageSwitch :trans="editTrans" />
              <FormField v-slot="f" :label="t('incidentDetail.fieldUpdateMessage')">
                <RichTextEditor :id="f.id" v-model="editMessage" class="form-textarea" :readonly="editOnDefaultLang" />
              </FormField>
              <FormLanguageDone :trans="editTrans" />
              <div class="form-actions">
                <OnyxButton :label="t('incidentDetail.saveTranslationsButton')" type="button" :loading="editSaving" data-test="update-translations-save" @click="saveTranslations" />
                <OnyxButton :label="t('common.cancel')" mode="outline" type="button" @click="closeTranslations" />
              </div>
            </div>
          </li>
        </ol>

        <div v-if="hasPerm('incidents:create')" id="update-form" class="update-form">
          <div v-if="updateAlert" class="alert alert-error">{{ updateAlert }}</div>
          <form novalidate @submit.prevent="submitUpdate">
            <FormField v-slot="f" required :label="t('incidentDetail.fieldNewStatus')">
              <select :id="f.id" v-model="updateForm.status" class="form-select" :required="f.required">
                <option v-for="opt in statusOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
              </select>
            </FormField>
            <FormLanguageSwitch :trans="trans" />
            <FormField v-slot="f" required :label="t('incidentDetail.fieldUpdateMessage')">
              <RichTextEditor :id="f.id" v-model="updateMessage" class="form-textarea" :required="f.required" :placeholder="t('incidentDetail.updateMessagePlaceholder')" />
            </FormField>
            <FormLanguageDone :trans="trans" />
            <div class="form-actions">
              <OnyxButton :label="t('incidentDetail.addUpdateButton')" type="submit" :loading="updateSaving" />
            </div>
          </form>
        </div>
      </section>

      <!-- Post-Mortem -->
      <section v-if="pm" class="ent-card body">
        <div class="pm-head">
          <h3 class="section-title">{{ t('incidentDetail.postMortemHeading') }}</h3>
          <StatusBadge v-if="pm.published_at" class="pm-state" status="operational" :label="t('incidentDetail.published')" />
          <StatusBadge v-else class="pm-state" :label="t('incidentDetail.draft')" />
          <span v-if="pm.published_at" class="pm-date mono">{{ fmtDay(pm.published_at) }}</span>
        </div>
        <p class="pm-summary">{{ pm.summary }}</p>

        <div class="pm-grid">
          <div v-for="field in pmFields" :key="field.key">
            <h4 class="pm-k">{{ field.label }}</h4>
            <p>{{ pm[field.key] }}</p>
          </div>
        </div>

        <template v-if="pm.actions && pm.actions.length">
          <h4 class="pm-k pm-k-spaced">{{ t('incidentDetail.followUpActions') }}</h4>
          <ul class="actions">
            <li v-for="a in pm.actions" :key="a.id" class="action-row">
              <span class="a-text">{{ a.text }}</span>
              <span class="a-owner">{{ a.owner }}</span>
              <StatusBadge :status="actionBadge(a.state, t).status" :label="actionBadge(a.state, t).label" />
            </li>
          </ul>
        </template>
      </section>

      <!-- Post-Mortem bearbeiten -->
      <section v-if="hasPerm('incidents:update')" class="ent-card body">
        <h3 class="section-title">{{ t('incidentDetail.editPostMortemHeading') }}</h3>
        <div v-if="pmAlert" class="alert alert-error">{{ pmAlert }}</div>
        <div v-if="pmNotice" class="alert alert-success" role="status">
          {{ pmNotice }} <button class="alert-close" @click="pmNotice = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
        </div>

        <FormField v-slot="f" :label="t('incidentDetail.summaryLabel')">
          <textarea :id="f.id" v-model="pmForm.summary" class="form-textarea" :placeholder="t('incidentDetail.summaryPlaceholder')"></textarea>
        </FormField>
        <div class="pm-grid">
          <FormField v-for="field in pmFields" :key="field.key" v-slot="f" :label="field.label">
            <textarea :id="f.id" v-model="pmForm[field.key]" class="form-textarea" :placeholder="field.placeholder"></textarea>
          </FormField>
        </div>

        <h4 class="pm-k pm-k-spaced">{{ t('incidentDetail.followUpActions') }}</h4>
        <ul v-if="pmForm.actions.length" class="actions">
          <li v-for="(a, idx) in pmForm.actions" :key="idx" class="action-row edit">
            <input v-model="a.text" class="form-input flex-1" :aria-label="t('incidentDetail.actionTextAria')" :placeholder="t('incidentDetail.actionTextPlaceholder')">
            <input v-model="a.owner" class="form-input w-owner" :aria-label="t('incidentDetail.actionOwnerAria')" :placeholder="t('incidentDetail.actionOwnerPlaceholder')">
            <select v-model="a.state" class="form-select w-state" :aria-label="t('incidentDetail.actionStateAria')">
              <option v-for="opt in actionStateOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
            <button type="button" class="action-remove" :aria-label="t('incidentDetail.removeActionAria')" :title="t('incidentDetail.removeActionAria')" @click="removeActionRow(idx)">
              <AppIcon name="x" :size="14" />
            </button>
          </li>
        </ul>
        <button type="button" class="action-add" @click="addActionRow">{{ t('incidentDetail.addActionButton') }}</button>

        <div class="form-actions">
          <OnyxButton :label="t('incidentDetail.saveDraftButton')" mode="outline" :loading="pmSaving" @click="submitPostMortem(false)" />
          <OnyxButton :label="t('incidentDetail.publishButton')" :loading="pmSaving" @click="submitPostMortem(true)" />
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.wrap { max-width: 1000px; }
.loading { margin: 0; padding: var(--sp-6); text-align: center; color: var(--text-2); }
.mono { font-family: var(--font-mono); }

.head { padding: var(--sp-5) var(--sp-6); margin-bottom: var(--sp-4); }
.head-meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr)); gap: var(--sp-4); margin: var(--sp-4) 0 0; padding-top: var(--sp-4); border-top: 1px solid var(--border); }
.m-k { font-size: var(--fs-xs); color: var(--text-2); font-weight: 600; }
.m-v { margin: var(--sp-1) 0 0; font-size: var(--fs-base); font-weight: 600; }

.body { padding: var(--sp-5) var(--sp-6); margin-bottom: var(--sp-4); }
.section-title { margin: 0 0 var(--sp-4); font-size: var(--fs-md); font-weight: 600; }

/* Zeitleiste: Linie links, Punkt je Eintrag */
.timeline { list-style: none; margin: 0; padding: 0; }
.tl-row { position: relative; padding: 0 0 var(--sp-5) var(--sp-5); border-inline-start: 2px solid var(--border); margin-inline-start: 6px; }
.tl-row::before { content: ''; position: absolute; inset-inline-start: -7px; top: 6px; inline-size: 12px; block-size: 12px; border-radius: 50%; background: var(--border-strong); border: 2px solid var(--surface); }
.tl-row:last-child { border-inline-start-color: transparent; padding-bottom: 0; }
.tl-head { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }
.tl-time { font-size: var(--fs-xs); color: var(--text-2); }
.tl-msg { font-size: var(--fs-base); line-height: 1.6; margin: var(--sp-2) 0 0; }
.tl-author { font-size: var(--fs-xs); color: var(--text-2); margin-top: var(--sp-2); }
.tl-actions { margin-top: var(--sp-2); }
.tl-trans { margin-top: var(--sp-3); padding: var(--sp-4); border: 1px solid var(--border); border-radius: var(--r-lg); }

.pm-head { display: flex; align-items: center; gap: var(--sp-2); margin-bottom: var(--sp-3); flex-wrap: wrap; }
.pm-head .section-title { margin: 0; }
.pm-date { margin-inline-start: auto; font-size: var(--fs-xs); color: var(--text-2); }
.pm-summary { font-size: var(--fs-base); line-height: 1.65; color: var(--text-2); margin: 0 0 var(--sp-5); }
.pm-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr)); gap: var(--sp-4); }
.pm-grid p { font-size: var(--fs-sm); line-height: 1.6; color: var(--text-2); margin: var(--sp-1) 0 0; }
.pm-k { margin: 0; font-size: var(--fs-sm); font-weight: 600; color: var(--text); }
.pm-k-spaced { margin-top: var(--sp-5); }

.actions { list-style: none; margin: var(--sp-2) 0 0; padding: 0; border: 1px solid var(--border); border-radius: var(--r-lg); overflow: hidden; }
.action-row { display: flex; align-items: center; gap: var(--sp-3); padding: var(--sp-3) var(--sp-4); border-bottom: 1px solid var(--border); }
.action-row:last-child { border-bottom: none; }
.a-text { font-size: var(--fs-sm); flex: 1; }
.a-owner { font-size: var(--fs-xs); color: var(--text-2); }

.update-form { margin-top: var(--sp-5); padding-top: var(--sp-5); border-top: 1px solid var(--border); }
.action-row.edit { gap: var(--sp-2); }
.flex-1 { flex: 1; }
.w-owner { width: 180px; }
.w-state { width: 150px; }
/* Eingaben und Auswahl der Folgemaßnahmen wie die übrigen kompakten Steuerelemente:
   kleinere Schrift (16 px war zu groß für die Zeile), feste Höhe, eigener Pfeil
   statt des Browser-Standards (Chevron aus zwei Verläufen in currentColor). */
.action-row.edit .form-input,
.action-row.edit .form-select {
  font-size: var(--fs-sm);
  height: var(--ctl-h);
  padding-top: 0;
  padding-bottom: 0;
}
.action-row.edit .form-select {
  appearance: none;
  -webkit-appearance: none;
  padding-right: 28px;
  background-image:
    linear-gradient(to top right, transparent calc(50% - 0.9px), currentColor calc(50% - 0.9px) calc(50% + 0.9px), transparent calc(50% + 0.9px)),
    linear-gradient(to bottom right, transparent calc(50% - 0.9px), currentColor calc(50% - 0.9px) calc(50% + 0.9px), transparent calc(50% + 0.9px));
  background-repeat: no-repeat;
  background-size: 5px 5px, 5px 5px;
  background-position: right 15px center, right 10px center;
}
.action-add {
  background: none; border: 1px dashed var(--border-strong); border-radius: var(--r-sm);
  color: var(--text-2); font-size: var(--fs-sm); padding: var(--sp-2) var(--sp-3); margin: var(--sp-2) 0 var(--sp-4); cursor: pointer;
}
.action-add:hover { border-color: var(--primary); color: var(--primary-text); }
.action-remove {
  background: none; border: none; color: var(--text-2); cursor: pointer; padding: var(--sp-1) 6px;
  display: flex; align-items: center;
}
.action-remove:hover { color: var(--major-text); }
</style>
