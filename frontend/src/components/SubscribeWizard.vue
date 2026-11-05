<script setup>
import { ref, computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import EmptyState from './EmptyState.vue'
import FormField from './FormField.vue'
import { useAuth } from '../composables/useAuth'
import { safeFetch, readJson } from '../composables/useApi'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  components: { type: Array, default: () => [] },
  slug: { type: String, required: true }
})
// Stryker restore all

const { t, te, locale } = useI18n()
const { token, currentUser } = useAuth()
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['close', 'subscribed'])

// Wizard State
const step = ref(1) // 1 = Kanal, 2 = Komponenten, 3 = Bestätigung
const channel = ref('') // 'email' | 'google_chat'
// eingeloggte Nutzer müssen ihre Konto-Adresse nicht neu
// eintippen, können sie aber weiterhin ändern (z.B. um eine andere Adresse
// zu abonnieren). Ausgeloggt bleibt das Feld wie bisher leer.
const email = ref(currentUser.value?.email ?? '')
const webhookURL = ref('')
const componentSearch = ref('')
const activeTag = ref('all')
const selectedComponentIDs = ref([])
const isLoading = ref(false)
const errorMsg = ref('')
// Der Serverfehler beim Abonnieren steht als Alert; er bekommt den Fokus,
// damit Screenreader- und Tastaturnutzer ihn bemerken.
const errorEl = ref(null)
// Stryker disable next-line BooleanLiteral: wird vor Schritt 3 immer aus der Antwort gesetzt
const pending = ref(false)

// Alle Komponenten flach (Baum aufgelöst) für die Auswahl
const flatComponents = computed(() => {
  const flatten = (nodes) => {
    let result = []
    for (const n of nodes) {
      if (n.Status !== 'group' && (!n.Children || n.Children.length === 0)) {
        result.push(n)
      }
      if (n.Children && n.Children.length > 0) {
        result = result.concat(flatten(n.Children))
      }
    }
    return result
  }
  return flatten(props.components)
})

// Ersatz für das frühere c.Country: Komponenten können jetzt
// mehrere Tags tragen statt eines einzelnen Country-Strings, daher Set über
// alle Tag-Namen statt über einen einzelnen Wert pro Komponente.
const availableCountries = computed(() => {
  const names = new Set()
  flatComponents.value.forEach(c => (c.Tags || []).forEach(t => names.add(t.Name)))
  return [...names].sort()
})

const filteredComponents = computed(() => {
  return flatComponents.value.filter(c => {
    const matchText = !componentSearch.value ||
      c.Name.toLowerCase().includes(componentSearch.value.toLowerCase())
    const matchTag = activeTag.value === 'all' || (c.Tags || []).some(t => t.Name === activeTag.value)
    return matchText && matchTag
  })
})

const isAllSelected = computed(() =>
  filteredComponents.value.length > 0 &&
  filteredComponents.value.every(c => selectedComponentIDs.value.includes(c.ID))
)

const toggleAll = () => {
  if (isAllSelected.value) {
    // Gefilterte abwählen
    const ids = new Set(filteredComponents.value.map(c => c.ID))
    selectedComponentIDs.value = selectedComponentIDs.value.filter(id => !ids.has(id))
  } else {
    // Alle gefilterten hinzufügen
    const newIds = filteredComponents.value.map(c => c.ID)
    selectedComponentIDs.value = [...new Set([...selectedComponentIDs.value, ...newIds])]
  }
}

// „Weiter“ bleibt immer bedienbar; ein deaktivierter Knopf ohne
// Begründung ließ offen, was fehlt. Stattdessen prüft validateStep1 beim Klick
// und nennt den Fehler am Feld (Muster FormField: role="alert", aria-invalid,
// aria-describedby) und setzt den Fokus aufs erste fehlerhafte Feld.
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const isValidWebhookURL = (value) => {
  try {
    return new URL(value).protocol === 'https:'
  } catch {
    return false
  }
}

const errors = ref({ channel: '', email: '', webhook: '' })
const bodyEl = ref(null)

const validateStep1 = () => {
  const next = { channel: '', email: '', webhook: '' }
  if (!channel.value) next.channel = t('components.subscribeWizard.errors.channelRequired')
  else if (channel.value === 'email') {
    if (!email.value.trim()) next.email = t('components.subscribeWizard.errors.emailRequired')
    else if (!EMAIL_RE.test(email.value.trim())) next.email = t('components.subscribeWizard.errors.emailInvalid')
  } else if (!webhookURL.value.trim()) next.webhook = t('components.subscribeWizard.errors.webhookRequired')
  else if (!isValidWebhookURL(webhookURL.value.trim())) next.webhook = t('components.subscribeWizard.errors.webhookInvalid')
  errors.value = next
  return !next.channel && !next.email && !next.webhook
}

const clearError = (key) => {
  if (errors.value[key]) errors.value = { ...errors.value, [key]: '' }
}

const selectChannel = (value) => {
  channel.value = value
  errors.value = { channel: '', email: '', webhook: '' }
}

const goStep2 = async () => {
  if (validateStep1()) {
    step.value = 2
    return
  }
  await nextTick()
  // Fokus aufs erste fehlerhafte Feld; ohne Kanal auf die erste Kanal-Karte.
  const first = bodyEl.value?.querySelector('[aria-invalid="true"]') ?? bodyEl.value?.querySelector('.channel-card')
  first?.focus()
}

// „0 / 8 ausgewählt“ las sich wie „nichts gewählt“, obwohl leer
// im Backend „alle Komponenten“ heißt. Der Zähler sagt jetzt, was gilt.
const selectionSummary = computed(() =>
  selectedComponentIDs.value.length === 0
    ? t('components.subscribeWizard.selectedNone')
    : t('components.subscribeWizard.selectedCount', { selected: selectedComponentIDs.value.length, total: flatComponents.value.length })
)

const selectedNames = computed(() =>
  flatComponents.value
    .filter(c => selectedComponentIDs.value.includes(c.ID))
    .map(c => c.Name)
)

const submit = async () => {
  isLoading.value = true
  errorMsg.value = ''
  try {
    const body = {
      channel: channel.value,
      email: channel.value === 'email' ? email.value : '',
      webhook_url: channel.value === 'google_chat' ? webhookURL.value : '',
      component_ids: selectedComponentIDs.value.length > 0
        ? selectedComponentIDs.value.join(',')
        : 'all',
      slug: props.slug,
      // Besuchersprache als Mailsprache; das Backend übernimmt sie nur,
      // wenn die Seite sie aktiviert hat (sonst Seitenstandardsprache).
      language: locale.value
    }
    // Mit Token, damit das Backend das Abo dem eingeloggten Konto zuordnet (Review-Befund 1)
    const headers = { 'Content-Type': 'application/json' }
    // X-Auth-Token statt Authorization (dort steht Caddys Basic Auth)
    if (token.value) headers['X-Auth-Token'] = token.value
    const res = await safeFetch('/api/subscribe', {
      method: 'POST',
      headers,
      body: JSON.stringify(body)
    })
    if (!res.ok) {
      // Kaputter/leerer Body (z. B. 502 vom Proxy) darf keinen rohen
      // JSON-Parserfehler anzeigen.
      const data = await res.json().catch(() => ({}))
      // stabiler Fehlercode -> api.errors.<code> (wie useApi), sonst Servertext.
      const key = typeof data.code === 'string' ? `api.errors.${data.code}` : ''
      throw new Error(key && te(key) ? t(key) : data.error || t('api.errors.server_error'))
    }
    // Unbestätigte E-Mail-Abos (Double-Opt-in) zeigen den Hinweis auf die Bestätigungsmail.
    const data = await readJson(res)
    pending.value = data.status === 'pending'
    step.value = 3
    emit('subscribed')
  } catch (e) {
    errorMsg.value = e.message
    await nextTick()
    errorEl.value?.focus()
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <!-- Backdrop -->
  <Teleport to="body">
    <div class="wizard-backdrop" @click.self="emit('close')">
      <dialog class="wizard-modal" open aria-modal="true" :aria-labelledby="`wizard-title-${step}`">

        <!-- Header -->
        <div class="wizard-header">
          <div class="wizard-steps">
            <span :class="['wizard-step-dot', step >= 1 ? 'active' : '']">1</span>
            <span class="wizard-step-line" :class="{ done: step >= 2 }"></span>
            <span :class="['wizard-step-dot', step >= 2 ? 'active' : '']">2</span>
            <span class="wizard-step-line" :class="{ done: step >= 3 }"></span>
            <span :class="['wizard-step-dot', step >= 3 ? 'active' : '']">
              <AppIcon v-if="step >= 3" name="check-circle" :size="14" />
              <template v-else>3</template>
            </span>
          </div>
          <button class="wizard-close" :aria-label="t('common.close')" @click="emit('close')">
            <AppIcon name="x" :size="16" />
          </button>
        </div>

        <!-- ======= SCHRITT 1: Kanal wählen ======= -->
        <Transition name="wizard-slide" mode="out-in">
          <div v-if="step === 1" key="step1" ref="bodyEl" class="wizard-body">
            <h2 id="wizard-title-1" class="wizard-title">{{ t('components.subscribeWizard.step1Title') }}</h2>
            <p class="wizard-subtitle">{{ t('components.subscribeWizard.step1Subtitle') }}</p>

            <div
              class="channel-cards"
              role="group"
              :aria-label="t('components.subscribeWizard.step1Title')"
              :aria-describedby="errors.channel ? 'subscribe-channel-error' : undefined"
            >
              <button
                class="channel-card"
                :class="{ selected: channel === 'email' }"
                :aria-pressed="channel === 'email'"
                @click="selectChannel('email')"
              >
                <span class="channel-icon"><AppIcon name="mail" :size="28" /></span>
                <strong>{{ t('components.subscribeWizard.channelEmailTitle') }}</strong>
                <small>{{ t('components.subscribeWizard.channelEmailDesc') }}</small>
              </button>
              <button
                class="channel-card"
                :class="{ selected: channel === 'google_chat' }"
                :aria-pressed="channel === 'google_chat'"
                @click="selectChannel('google_chat')"
              >
                <span class="channel-icon"><AppIcon name="message-circle" :size="28" /></span>
                <strong>{{ t('components.subscribeWizard.channelChatTitle') }}</strong>
                <small>{{ t('components.subscribeWizard.channelChatDesc') }}</small>
              </button>
            </div>

            <p v-if="errors.channel" id="subscribe-channel-error" class="wizard-field-error" role="alert">
              <AppIcon name="alert-circle" :size="14" />{{ errors.channel }}
            </p>

            <!-- Eingabe je nach Kanal -->
            <Transition name="wizard-fade" mode="out-in">
              <div v-if="channel === 'email'" key="email-input" class="channel-input">
                <FormField
                  v-slot="f"
                  :label="t('components.subscribeWizard.emailLabel')"
                  required
                  :error="errors.email"
                  :field-id="'subscribe-email'"
                >
                  <input
                    :id="f.id"
                    v-model="email"
                    type="email"
                    :placeholder="t('components.subscribeWizard.emailPlaceholder')"
                    autocomplete="email"
                    :required="f.required"
                    :aria-describedby="f.describedby"
                    :aria-invalid="f.invalid"
                    @input="clearError('email')"
                    @keyup.enter="goStep2"
                  />
                </FormField>
              </div>
              <div v-else-if="channel === 'google_chat'" key="webhook-input" class="channel-input">
                <FormField
                  v-slot="f"
                  :label="t('components.subscribeWizard.webhookLabel')"
                  required
                  :error="errors.webhook"
                  :hint="`${t('components.subscribeWizard.webhookHint')} ${t('components.subscribeWizard.webhookHintEm')}`"
                  :field-id="'subscribe-webhook-url'"
                >
                  <input
                    :id="f.id"
                    v-model="webhookURL"
                    type="url"
                    placeholder="https://chat.googleapis.com/v1/spaces/.../messages?key=..."
                    :required="f.required"
                    :aria-describedby="f.describedby"
                    :aria-invalid="f.invalid"
                    @input="clearError('webhook')"
                    @keyup.enter="goStep2"
                  />
                </FormField>
              </div>
            </Transition>

            <div class="wizard-footer">
              <button class="btn-ghost" @click="emit('close')">{{ t('common.cancel') }}</button>
              <button
                class="btn-primary"
                @click="goStep2"
              >{{ t('components.subscribeWizard.next') }}</button>
            </div>
          </div>
        </Transition>

        <!-- ======= SCHRITT 2: Komponenten wählen ======= -->
        <Transition name="wizard-slide" mode="out-in">
          <div v-if="step === 2" key="step2" class="wizard-body">
            <h2 id="wizard-title-2" class="wizard-title">{{ t('components.subscribeWizard.step2Title') }}</h2>
            <p class="wizard-subtitle">
              {{ t('components.subscribeWizard.step2Subtitle') }}
            </p>

            <!-- Such & Filter -->
            <div class="comp-filter">
              <div class="comp-search-wrap">
                <span class="search-icon-sm"><AppIcon name="search" :size="14" /></span>
                <input
                  v-model="componentSearch"
                  type="text"
                  class="comp-search-input"
                  :placeholder="t('components.subscribeWizard.searchPlaceholder')"
                  :aria-label="t('components.subscribeWizard.searchAria')"
                />
              </div>
              <div class="comp-tags">
                <button
                  class="filter-tag"
                  :class="{ active: activeTag === 'all' }"
                  @click="activeTag = 'all'"
                >{{ t('common.all') }}</button>
                <button
                  v-for="country in availableCountries"
                  :key="country"
                  class="filter-tag"
                  :class="{ active: activeTag === country }"
                  @click="activeTag = country"
                >{{ country }}</button>
              </div>
            </div>

            <!-- Alle auswählen -->
            <div class="select-all-row">
              <label class="perm-checkbox">
                <input type="checkbox" :checked="isAllSelected" @change="toggleAll" />
                <span>{{ isAllSelected ? t('components.subscribeWizard.deselectAll') : t('components.subscribeWizard.selectAll') }}</span>
              </label>
              <span class="selected-count" aria-live="polite">{{ selectionSummary }}</span>
            </div>

            <!-- Komponenten-Liste -->
            <div class="comp-list">
              <label
                v-for="c in filteredComponents"
                :key="c.ID"
                class="comp-list-item"
                :class="{ checked: selectedComponentIDs.includes(c.ID) }"
              >
                <input
                  type="checkbox"
                  :value="c.ID"
                  v-model="selectedComponentIDs"
                />
                <span class="comp-list-name">{{ c.Name }}</span>
                <span class="meta-tag wizard-comp-tag">{{ (c.Tags || []).map(tag => tag.Name).join(', ') }}</span>
              </label>
              <EmptyState
                v-if="filteredComponents.length === 0"
                icon="search"
                :title="t('components.subscribeWizard.noMatches')"
              />
            </div>

            <p v-if="errorMsg" ref="errorEl" class="wizard-alert" role="alert" tabindex="-1">
              <AppIcon name="alert-triangle" :size="14" /> {{ errorMsg }}
            </p>
            <div class="wizard-footer">
              <button class="btn-ghost" @click="step = 1">{{ t('components.subscribeWizard.back') }}</button>
              <button
                class="btn-primary"
                :disabled="isLoading"
                :aria-busy="isLoading"
                @click="submit"
              >
                <span v-if="isLoading" class="btn-inline-content">
                  <span class="btn-spinner" aria-hidden="true"></span> {{ t('components.subscribeWizard.saving') }}
                </span>
                <span v-else class="btn-inline-content">
                  {{ t('components.subscribeWizard.subscribeButton') }} <AppIcon name="check-circle" :size="15" />
                </span>
              </button>
            </div>
          </div>
        </Transition>

        <!-- ======= SCHRITT 3: Bestätigung ======= -->
        <Transition name="wizard-slide" mode="out-in">
          <div v-if="step === 3" key="step3" class="wizard-body wizard-success">
            <div class="success-icon"><AppIcon name="check-circle" :size="48" /></div>
            <h2 id="wizard-title-3" class="wizard-title">{{ pending ? t('subscribeConfirm.pendingTitle') : t('components.subscribeWizard.successTitle') }}</h2>
            <p v-if="pending" class="wizard-subtitle">{{ t('subscribeConfirm.pendingMessage', { email }) }}</p>
            <p v-else class="wizard-subtitle">
              {{ t('components.subscribeWizard.successSubtitlePre') }}
              <strong>{{ channel === 'email' ? email : 'Google Chat' }}</strong>
              {{ t('components.subscribeWizard.successSubtitlePost') }}
            </p>
            <div class="success-components">
              <span
                v-for="name in selectedNames"
                :key="name"
                class="filter-tag active"
              >{{ name }}</span>
              <span v-if="selectedComponentIDs.length === 0" class="filter-tag active">
                {{ t('components.subscribeWizard.allServices') }}
              </span>
            </div>
            <button class="btn-primary wizard-done-btn" @click="emit('close')">
              {{ t('components.subscribeWizard.done') }}
            </button>
          </div>
        </Transition>

      </dialog>
    </div>
  </Teleport>
</template>

<style scoped>
/* Backdrop */
.wizard-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(6px);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

/* Modal — native <dialog> element, styled to match the previous div */
.wizard-modal {
  position: relative;
  padding: 0;
  margin: 0;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 20px;
  width: 100%;
  max-width: 540px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.25);
  overflow: hidden;
  color: inherit;
}

/* Header mit Steps */
.wizard-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 24px 0;
}

.wizard-steps {
  display: flex;
  align-items: center;
  gap: 6px;
}

.wizard-step-dot {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--bg);
  border: 2px solid var(--border);
  color: var(--text-3);
  font-size: 0.8rem;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.3s;
}

.wizard-step-dot.active {
  background: var(--primary);
  border-color: var(--primary);
  color: white;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.2);
}

.wizard-step-line {
  width: 32px;
  height: 2px;
  background: var(--border);
  border-radius: 2px;
  transition: background 0.3s;
}

.wizard-step-line.done {
  background: var(--primary);
}

.wizard-close {
  background: none;
  border: none;
  color: var(--text-3);
  font-size: 1.1rem;
  cursor: pointer;
  padding: 6px;
  border-radius: 8px;
  transition: all 0.2s;
}

.wizard-close:hover {
  background: var(--bg);
  color: var(--text);
}

/* Body */
.wizard-body {
  padding: 24px 28px 28px;
}

.wizard-title {
  font-size: 1.25rem;
  font-weight: 700;
  color: var(--text);
  margin: 0 0 8px;
}

.wizard-subtitle {
  font-size: 0.9rem;
  color: var(--text-secondary);
  margin: 0 0 24px;
  line-height: 1.5;
}

/* Kanal-Karten */
.channel-cards {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-bottom: 20px;
}

.channel-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 20px 16px;
  background: var(--bg);
  border: 2px solid var(--border);
  border-radius: 14px;
  cursor: pointer;
  transition: all 0.2s;
  font-family: inherit;
  color: var(--text);
  text-align: center;
}

.channel-card:hover {
  border-color: var(--primary);
  transform: translateY(-2px);
}

.channel-card.selected {
  border-color: var(--primary);
  background: rgba(99, 102, 241, 0.06);
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1);
}

.channel-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--primary);
}

.channel-card strong {
  font-size: 0.95rem;
}

.channel-card small {
  font-size: 0.78rem;
  color: var(--text-3);
  line-height: 1.3;
}

/* Kanal-Eingabe */
.channel-input {
  margin-bottom: 8px;
}

.channel-input :deep(input) {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 12px 14px;
  min-height: 44px;
  color: var(--text);
  font-size: 0.95rem;
  font-family: inherit;
  outline: none;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.channel-input :deep(input:focus) {
  border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.12);
}

.channel-input :deep(.form-field--invalid input) {
  border-color: var(--major);
}

/* Fehler zur Kanalwahl (Schritt 1, noch kein Kanal gewählt) */
.wizard-field-error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 0 0 12px;
  font-size: var(--fs-sm);
  color: var(--major-text);
}

/* Komponenten-Filter */
.comp-filter {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 12px;
}

.comp-search-wrap {
  position: relative;
}

.search-icon-sm {
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-3);
  display: flex;
  pointer-events: none;
}

.comp-search-input {
  width: 100%;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px 12px 10px 36px;
  color: var(--text);
  font-size: 0.9rem;
  font-family: inherit;
  outline: none;
  box-sizing: border-box;
  transition: border-color 0.2s;
}

.comp-search-input:focus {
  border-color: var(--primary);
}

.comp-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

/* Alle auswählen Zeile */
.select-all-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: var(--bg);
  border-radius: 8px;
  margin-bottom: 8px;
}

.selected-count {
  font-size: 0.8rem;
  color: var(--text-3);
}

/* Komponenten-Liste */
.comp-list {
  max-height: 220px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 12px;
  margin-bottom: 20px;
}

.comp-list-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  cursor: pointer;
  transition: background 0.15s;
  border-bottom: 1px solid var(--border);
}

.comp-list-item:last-child {
  border-bottom: none;
}

.comp-list-item:hover {
  background: var(--bg);
}

.comp-list-item.checked {
  background: rgba(99, 102, 241, 0.05);
}

.comp-list-item input[type="checkbox"] {
  accent-color: var(--primary);
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.comp-list-name {
  font-size: 0.9rem;
  color: var(--text);
  flex: 1;
}

/* Footer */
.wizard-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

.btn-primary {
  background: var(--primary);
  color: var(--on-primary);
  border: none;
  border-radius: var(--radius, 6px);
  padding: 11px 24px;
  font-size: 0.95rem;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-inline-content {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.btn-spinner {
  width: 13px;
  height: 13px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: var(--on-primary);
  animation: btn-spin 0.7s linear infinite;
}

@keyframes btn-spin {
  to { transform: rotate(360deg); }
}

.wizard-comp-tag { margin-inline-start: auto; flex-shrink: 0; }
.wizard-done-btn { margin-top: var(--sp-5); }

.btn-primary:hover:not(:disabled) {
  background: var(--primary-hover);
}

.btn-primary:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.btn-ghost {
  background: none;
  color: var(--text-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 11px 20px;
  font-size: 0.95rem;
  cursor: pointer;
  font-family: inherit;
  transition: all 0.2s;
}

.btn-ghost:hover {
  border-color: var(--text-secondary);
  color: var(--text);
}

.wizard-alert {
  margin: 10px 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 0.85rem;
  color: var(--major-text);
}

/* Erfolg */
.wizard-success {
  text-align: center;
  padding-top: 32px;
}

.success-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--ok-text);
  margin-bottom: 16px;
}

.success-components {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
  margin-top: 16px;
}

/* Animationen */
.wizard-slide-enter-active,
.wizard-slide-leave-active {
  transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}
.wizard-slide-enter-from {
  opacity: 0;
  transform: translateX(20px);
}
.wizard-slide-leave-to {
  opacity: 0;
  transform: translateX(-20px);
}

.wizard-fade-enter-active,
.wizard-fade-leave-active {
  transition: all 0.2s;
}
.wizard-fade-enter-from,
.wizard-fade-leave-to {
  opacity: 0;
  transform: translateY(6px);
}
</style>
