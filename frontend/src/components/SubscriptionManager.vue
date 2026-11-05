<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../composables/useApi'
import AppIcon from './AppIcon.vue'
import { usePublicPageLanguages } from '../composables/usePublicPageLanguages'
import { hasLanguageChoice, subscriberLanguageOptions, selectableSubscriberLanguage } from '../utils/subscriberLanguage'

const { apiFetch } = useApi()
const { t } = useI18n()
// Sprachen der Seite (aus dem Dashboard, setzt StatusPage.vue) für die Mailsprache.
const { pageLanguages } = usePublicPageLanguages()

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  isLoggedIn: {
    type: Boolean,
    required: true
  },
  currentUser: {
    type: Object,
    default: null
  },
  components: {
    type: Array,
    required: true
  },
  slug: {
    type: String,
    required: true
  }
})
// Stryker restore all

// Stryker disable next-line all: defineEmits wird von Vue aus der Setup-Funktion gehoben, instrumentierter Code darf dort nicht stehen
const emit = defineEmits(['open-wizard', 'subscription-state'])

const showEditAbo = ref(false)
const editSub = ref(null)
const editLoading = ref(false)
const editMsg = ref('')
const editMsgOk = ref(false)
const editSelectedIDs = ref([])
// Mailsprache ('' = Standardsprache der Seite).
const editLanguage = ref('')
const showLanguageChoice = computed(() => hasLanguageChoice(pageLanguages.value))
const languageOptions = computed(() =>
  subscriberLanguageOptions(pageLanguages.value, (name) => t('components.subscriptionManager.languageDefault', { name }))
)
// "kein Abonnement" (404) ist ein Normalzustand, ein Ladefehler nicht.
const noSubscription = ref(false)
// Stryker disable next-line BooleanLiteral: Startwert äquivalent, loadSubscription() setzt ihn vor jedem Lesen zurück
const loadFailed = ref(false)
// "Abonnement beenden" - Bestätigung läuft als eigener
// Zustand auf der Seite statt window.confirm (per Spec nicht erlaubt, u.a.
// weil es sich nicht stylen/testen lässt).
// Stryker disable next-line BooleanLiteral: Startwert äquivalent, loadSubscription() setzt ihn vor jedem Lesen zurück
const endConfirmOpen = ref(false)
const endLoading = ref(false)
const bodyPanelRef = ref(null)

// Alle echten Blatt-Komponenten (flach) für Abo-Edit
const flatComponents = computed(() => {
  const flatten = (nodes) => {
    let result = []
    for (const n of nodes) {
      if (!n.Children || n.Children.length === 0) result.push(n)
      if (n.Children) result = result.concat(flatten(n.Children))
    }
    return result
  }
  return flatten(props.components)
})

// Lädt das eigene Abonnement. 404 = kein Abonnement (kein Fehler, die Statusseite
// selbst ist ja geladen); alles andere (500, Netzwerk, ...) ist ein echter Fehler.
const loadSubscription = async () => {
  editLoading.value = true
  loadFailed.value = false
  endConfirmOpen.value = false
  try {
    // 404 ist der Normalfall (kein Abo) und kein Konsolenfehler
    const res = await apiFetch(`/api/me/subscription?slug=${encodeURIComponent(props.slug)}`, { expectedStatuses: [404] })
    editSub.value = res
    editSelectedIDs.value = res.component_ids === 'all' ? [] : res.component_ids.split(',').map(Number)
    editLanguage.value = selectableSubscriberLanguage(res.language ?? '', pageLanguages.value)
    noSubscription.value = false
    emit('subscription-state', true)
  } catch (err) {
    if (err.status === 404) {
      editSub.value = null
      noSubscription.value = true
      emit('subscription-state', false)
    } else {
      // Ein früheres 404 gilt nicht mehr: Der Zustand ist jetzt unbekannt (null),
      // sonst hätte "noch kein Abonnement" im Template Vorrang vor dem Fehler.
      if (noSubscription.value) emit('subscription-state', null)
      noSubscription.value = false
      loadFailed.value = true
    }
  } finally {
    editLoading.value = false
  }
}

const openEditAbo = async () => {
  if (!props.isLoggedIn) return
  showEditAbo.value = true
  editMsg.value = ''
  await loadSubscription()
}

const toggleEditAbo = async () => {
  if (showEditAbo.value) {
    showEditAbo.value = false
    return
  }
  await openEditAbo()
  // Barrierefreiheit: Tastaturbedienung braucht einen sichtbaren
  // Fokus-Sprung in den gerade aufgeklappten Bereich, statt dass der Fokus auf
  // der (jetzt expandierten) Überschrift verharrt.
  await nextTick()
  // Stryker disable next-line OptionalChaining: toggleEditAbo wird nur über den Header gerufen, der nur bei isLoggedIn rendert - bodyPanelRef ist dann immer gesetzt
  bodyPanelRef.value?.focus()
}

// Stiller Vorab-Check nach dem Einloggen: die Statusseite braucht den Zustand,
// um nur passende Bedienelemente zu zeigen. Der Bereich bleibt dabei zu.
watch(() => props.isLoggedIn, (loggedIn) => {
  if (loggedIn) loadSubscription()
}, { immediate: true })

// Erlaubt der übergeordneten StatusPage, den Editor direkt aus dem CTA heraus
// zu öffnen (openEditAbo) und den Zustand nach dem Abonnieren neu zu holen (refresh).
defineExpose({ openEditAbo, refresh: loadSubscription })

const saveEditAbo = async () => {
  editLoading.value = true
  try {
    await apiFetch(`/api/me/subscription?slug=${encodeURIComponent(props.slug)}`, {
      method: 'PUT',
      body: JSON.stringify({
        component_ids: editSelectedIDs.value.length ? editSelectedIDs.value.join(',') : 'all',
        // nur mitsenden, wenn die Wahl sichtbar ist; sonst bliebe sie
        // serverseitig unverändert (fehlendes Feld) statt auf '' zurückzufallen.
        ...(showLanguageChoice.value ? { language: editLanguage.value } : {})
      })
    })
    editMsg.value = t('components.subscriptionManager.savedMsg')
    editMsgOk.value = true
    showEditAbo.value = false
  } catch (e) {
    editMsg.value = t('components.subscriptionManager.saveFailedMsg')
    editMsgOk.value = false
  } finally {
    editLoading.value = false
  }
}

// löscht das eigene Abonnement und führt den Bereich
// zurück in Zustand B ("kein Abonnement") statt ihn einfach zuzuklappen -
// der Nutzer bleibt im Kontext und sieht sofort, dass es funktioniert hat.
const confirmEndSubscription = async () => {
  endLoading.value = true
  try {
    await apiFetch(`/api/me/subscription?slug=${encodeURIComponent(props.slug)}`, { method: 'DELETE' })
    editSub.value = null
    noSubscription.value = true
    emit('subscription-state', false)
  } catch (e) {
    editMsg.value = t('components.subscriptionManager.endFailedMsg')
    editMsgOk.value = false
    endConfirmOpen.value = false
  } finally {
    endLoading.value = false
  }
}
</script>

<template>
  <div class="edit-abo-section" v-if="isLoggedIn && (!noSubscription || showEditAbo)">
    <button
      type="button"
      class="edit-abo-header"
      :aria-expanded="showEditAbo ? 'true' : 'false'"
      aria-controls="edit-abo-body-panel"
      @click="toggleEditAbo"
    >
      <span class="edit-abo-header-label">
        <AppIcon name="settings" :size="15" /> {{ t('components.subscriptionManager.editSectionTitle') }}
      </span>
      <span class="toggle-chevron" :class="{ open: showEditAbo }">
        <AppIcon name="chevron-right" :size="14" />
      </span>
    </button>

    <!-- Außerhalb des einklappbaren Bereichs, damit die Bestätigung nach dem Speichern sichtbar bleibt -->
    <p
      v-if="editMsg"
      class="edit-abo-msg"
      :class="{ 'edit-abo-msg--ok': editMsgOk }"
      role="status"
    >
      <AppIcon :name="editMsgOk ? 'check-circle' : 'alert-triangle'" :size="14" />
      {{ editMsg }}
    </p>

    <Transition name="tree-slide">
      <div
        v-if="showEditAbo"
        id="edit-abo-body-panel"
        ref="bodyPanelRef"
        class="edit-abo-body"
        tabindex="-1"
      >
        <p class="edit-abo-intro">
          {{ t('components.subscriptionManager.loggedInAs') }} <strong>{{ currentUser?.email }}</strong>.
        </p>
        <p v-if="editLoading && !editSub" class="edit-abo-loading" role="status">
          <span class="btn-spinner-sm" aria-hidden="true"></span> {{ t('components.subscriptionManager.loadingEllipsis') }}
        </p>
        <p v-else-if="noSubscription" class="edit-abo-none">
          {{ t('components.subscriptionManager.noSubscriptionYet') }}
        </p>
        <div v-else-if="loadFailed" class="edit-abo-error" role="alert">
          <AppIcon name="alert-triangle" :size="14" />
          {{ t('components.subscriptionManager.loadFailedMsg') }}
          <button class="btn-ghost-sm btn-retry-abo" :disabled="editLoading" @click="loadSubscription">
            {{ t('components.subscriptionManager.retryButton') }}
          </button>
        </div>

        <!-- Ergebnis: Abo bearbeiten -->
        <div v-if="editSub" class="edit-abo-result">
          <div class="edit-abo-result-header">
            <div>
              <span class="channel-label">
                {{ t('components.subscriptionManager.channelLabel') }}
                <strong class="channel-label-value">
                  <AppIcon :name="editSub.channel === 'google_chat' ? 'message-circle' : 'mail'" :size="14" />
                  {{ editSub.channel === 'google_chat' ? 'Google Chat' : t('components.subscriptionManager.channelEmailLabel') }}
                </strong>
              </span>
              <div v-if="editSub.channel === 'google_chat'" class="edit-abo-webhook">
                {{ t('components.subscriptionManager.webhookLabel') }} {{ editSub.webhook_url }}
              </div>
            </div>
            <button class="btn-ghost-sm" @click="emit('open-wizard'); editSub = null; endConfirmOpen = false">
              {{ t('components.subscriptionManager.changeChannelButton') }}
            </button>
          </div>
          <p class="edit-abo-hint">{{ t('components.subscriptionManager.selectComponents') }}</p>
          <div class="edit-abo-matrix">
            <label v-for="c in flatComponents" :key="c.ID" class="perm-checkbox">
              <input type="checkbox" :value="c.ID" v-model="editSelectedIDs" />
              {{ c.Name }}
            </label>
          </div>
          <div v-if="showLanguageChoice" class="edit-abo-language">
            <label for="edit-abo-language-select" class="edit-abo-language-label">{{ t('components.subscriptionManager.languageLabel') }}</label>
            <select id="edit-abo-language-select" v-model="editLanguage" class="edit-abo-language-select" aria-describedby="edit-abo-language-hint">
              <option v-for="o in languageOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
            <p id="edit-abo-language-hint" class="edit-abo-language-hint">{{ t('components.subscriptionManager.languageHint') }}</p>
          </div>
          <div class="edit-abo-actions">
            <button
              class="btn-save-abo"
              :disabled="editLoading"
              :aria-busy="editLoading"
              @click="saveEditAbo"
            >
              <span v-if="editLoading" class="btn-inline-content">
                <span class="btn-spinner-sm" aria-hidden="true"></span> {{ t('components.subscriptionManager.savingEllipsis') }}
              </span>
              <span v-else class="btn-inline-content">
                <AppIcon name="check-circle" :size="14" /> {{ t('common.save') }}
              </span>
            </button>
            <button class="btn-ghost-sm" @click="editSub = null; endConfirmOpen = false">{{ t('common.cancel') }}</button>
          </div>

          <!-- "Abonnement beenden" - Bestätigung läuft als
               eigener Zustand auf der Seite statt window.confirm. -->
          <div class="edit-abo-end-section">
            <button
              v-if="!endConfirmOpen"
              type="button"
              class="btn-ghost-sm btn-end-abo"
              @click="endConfirmOpen = true"
            >
              <AppIcon name="x" :size="14" /> {{ t('components.subscriptionManager.endSubscriptionButton') }}
            </button>
            <div v-else class="edit-abo-end-confirm" role="alertdialog" :aria-label="t('components.subscriptionManager.endSubscriptionButton')">
              <p class="edit-abo-end-confirm-text">{{ t('components.subscriptionManager.endConfirmText') }}</p>
              <div class="edit-abo-end-confirm-actions">
                <button
                  type="button"
                  class="btn-end-confirm"
                  :disabled="endLoading"
                  :aria-busy="endLoading"
                  @click="confirmEndSubscription"
                >
                  {{ t('components.subscriptionManager.endConfirmYes') }}
                </button>
                <button type="button" class="btn-ghost-sm" :disabled="endLoading" @click="endConfirmOpen = false">
                  {{ t('common.cancel') }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.edit-abo-section {
  margin-top: 20px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  overflow: hidden;
}

.edit-abo-header {
  width: 100%;
  padding: 16px 20px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 0.9rem;
  font-weight: 600;
  font-family: inherit;
  color: var(--text-secondary);
  cursor: pointer;
  background: var(--bg);
  border: none;
  user-select: none;
}

.edit-abo-header:focus-visible,
.btn-retry-abo:focus-visible,
.btn-end-abo:focus-visible,
.btn-end-confirm:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}

.edit-abo-header:hover {
  color: var(--text);
}

.edit-abo-header-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.toggle-chevron {
  display: inline-flex;
  transition: transform 0.2s;
}

.toggle-chevron.open {
  transform: rotate(90deg);
}

.edit-abo-body {
  padding: 20px;
  border-top: 1px solid var(--border);
}

.btn-inline-content {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.btn-spinner-sm {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid rgba(120, 120, 120, 0.3);
  border-top-color: var(--text);
  animation: abo-spin 0.7s linear infinite;
}

@keyframes abo-spin {
  to { transform: rotate(360deg); }
}

.edit-abo-msg {
  margin: 0;
  padding: 10px 20px;
  font-size: 0.85rem;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--major-text);
}

.edit-abo-msg--ok {
  color: var(--ok-text);
}

.edit-abo-none,
.edit-abo-loading {
  font-size: 0.88rem;
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  gap: 8px;
}

.edit-abo-error {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  font-size: 0.88rem;
  color: var(--major-text);
}

.channel-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.channel-label-value {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.edit-abo-result {
  margin-top: 24px;
  padding-top: 20px;
  border-top: 1px solid var(--border);
}

.edit-abo-result-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
  font-size: 0.95rem;
  color: var(--text);
}

/* "Abonnement beenden" */
.edit-abo-end-section {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
}

.btn-end-abo {
  color: var(--major-text);
  border-color: var(--major-text);
}

.edit-abo-end-confirm-text {
  font-size: 0.88rem;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.edit-abo-end-confirm-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.btn-end-confirm {
  background: var(--major-text);
  color: var(--icon-on);
  border: none;
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
}
.btn-end-confirm:hover:not(:disabled) {
  filter: brightness(0.9);
}
.btn-end-confirm:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-ghost-sm {
  background: none;
  border: 1px solid var(--border);
  color: var(--text-secondary);
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 0.85rem;
  cursor: pointer;
  transition: all 0.2s;
}
.btn-ghost-sm:hover {
  border-color: var(--text);
  color: var(--text);
}

.btn-save-abo {
  background: var(--ok);
  color: var(--icon-on);
  border: none;
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
  display: inline-flex;
  align-items: center;
}
.btn-save-abo:hover:not(:disabled) {
  filter: brightness(0.9);
}
.btn-save-abo:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.edit-abo-intro { margin: 0 0 var(--sp-3); font-size: var(--fs-base); color: var(--text-secondary); }
.edit-abo-webhook { margin-top: var(--sp-1); font-size: var(--fs-sm); color: var(--text-3); word-break: break-all; }
.edit-abo-hint { margin: 0 0 var(--sp-3); font-size: var(--fs-base); color: var(--text-3); }
.edit-abo-matrix { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: var(--sp-2); }
.edit-abo-language { margin-top: var(--sp-4); display: flex; flex-direction: column; gap: var(--sp-1); max-width: 320px; }
.edit-abo-language-label { font-size: var(--fs-base); color: var(--text-secondary); font-weight: 600; }
.edit-abo-language-select { padding: 6px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface); color: var(--text); font: inherit; }
.edit-abo-language-hint { margin: 0; font-size: var(--fs-sm); color: var(--text-3); }
.edit-abo-actions { margin-top: var(--sp-4); display: flex; flex-wrap: wrap; gap: var(--sp-3); }
</style>
