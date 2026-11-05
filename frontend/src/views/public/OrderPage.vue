<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { serverInputErrorText } from '../../utils/inputRules'
import { primaryBaseDomain } from '../../utils/appDomains'
import '../../assets/entry.css'

const { t } = useI18n()
const { apiFetch } = useApi()

// Ist die Self-Service-Bestellung deaktiviert, zeigt die Seite statt
// des Formulars einen Hinweis. Default true (optimistisch) - das Backend lehnt
// einen Absenden-Versuch im Zweifel ohnehin mit 403 ab.
const ordersEnabled = ref(true)

onMounted(async () => {
  try {
    const flag = await apiFetch('/api/public/orders/enabled')
    ordersEnabled.value = flag.enabled
  } catch (err) {
    console.error('Failed to load orders flag', err)
  }
})

const companyName = ref('')
const email = ref('')
const slug = ref('')
// Beispieladresse im Hinweis: <Wunsch>.<Basisdomain>, ohne Basisdomain der Pfad /status/<Wunsch>.
const slugHint = computed(() => {
  const base = primaryBaseDomain()
  const example = t('order.slugExample')
  return t('order.slugHint', { address: base ? `${example}.${base}` : `/status/${example}` })
})
// Honeypot: echte Nutzer sehen/befüllen dieses Feld nie (per CSS
// versteckt + tabindex="-1"). Ein befüllter Wert lässt das Backend die
// Bestellung stillschweigend verwerfen, ohne dem Bot ein Erkennungssignal zu
// geben - die Erfolgsmeldung bleibt identisch.
const website = ref('')

const submitting = ref(false)
const submitted = ref(false)
const errorMessage = ref('')

async function submitOrder() {
  errorMessage.value = ''
  submitting.value = true
  try {
    await apiFetch('/api/public/orders', {
      method: 'POST',
      body: JSON.stringify({
        company_name: companyName.value,
        email: email.value,
        slug: slug.value,
        website: website.value,
      }),
    })
    submitted.value = true
  } catch (err) {
    errorMessage.value = serverInputErrorText(err, t) || err.message
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="order-page entry-page">
    <div class="order-card entry-card entry-card--wide">
      <div v-if="!ordersEnabled" class="order-disabled">
        <h1 class="entry-title">{{ t('order.ordersDisabledTitle') }}</h1>
        <p class="entry-subtitle">{{ t('order.ordersDisabledMessage') }}</p>
      </div>
      <div v-else-if="submitted" class="order-success">
        <h1 class="entry-title">{{ t('order.successTitle') }}</h1>
        <p class="entry-subtitle">{{ t('order.successMessage') }}</p>
      </div>
      <template v-else>
        <h1 class="entry-title">{{ t('order.pageTitle') }}</h1>
        <p class="order-subtitle entry-subtitle">{{ t('order.pageSubtitle') }}</p>

        <div v-if="errorMessage" class="order-error entry-alert" role="alert">{{ t('order.errorGeneric', { message: errorMessage }) }}</div>

        <form novalidate class="order-form entry-form" @submit.prevent="submitOrder">
          <p class="entry-hint" data-test="required-hint">{{ t('patterns.requiredHint') }}</p>
          <div class="entry-field">
            <label class="onyx-required-marker" for="order-company">{{ t('order.companyNameLabel') }}</label>
            <input
              id="order-company"
              v-model="companyName"
              type="text"
              required
              aria-required="true"
              :placeholder="t('order.companyNamePlaceholder')"
              autocomplete="organization"
            />
          </div>
          <div class="entry-field">
            <label class="onyx-required-marker" for="order-email">{{ t('order.emailLabel') }}</label>
            <input
              id="order-email"
              v-model="email"
              type="email"
              required
              aria-required="true"
              :placeholder="t('order.emailPlaceholder')"
              autocomplete="email"
            />
          </div>
          <div class="entry-field">
            <label for="order-slug">{{ t('order.slugLabel') }}</label>
            <input id="order-slug" v-model="slug" type="text" :placeholder="t('order.slugPlaceholder')" autocomplete="off" />
            <p class="field-hint entry-hint">{{ slugHint }}</p>
          </div>

          <!-- Honeypot: für echte Nutzer per CSS unsichtbar, aber im DOM vorhanden -->
          <div class="honeypot-field entry-honeypot" aria-hidden="true">
            <label for="order-website">{{ t('order.honeypotLabel') }}</label>
            <input id="order-website" v-model="website" type="text" tabindex="-1" autocomplete="off" />
          </div>

          <button class="btn-order-submit entry-btn" type="submit" :disabled="submitting">
            {{ submitting ? t('order.submitting') : t('order.submitButton') }}
          </button>
        </form>
      </template>
    </div>
  </div>
</template>
