<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { serverInputErrorText } from '../../utils/inputRules'
import { primaryBaseDomain } from '../../utils/appDomains'
import '../../assets/entry.css'

const { t } = useI18n()
const { apiFetch } = useApi()

// When self-service ordering is disabled, the page shows a notice instead
// of the form. Default true (optimistic) - the backend rejects
// a submit attempt with 403 anyway if in doubt.
const ordersEnabled = ref(true)

onMounted(async () => {
  try {
    const flag = await apiFetch<{ enabled: boolean }>('/api/public/orders/enabled')
    ordersEnabled.value = flag.enabled
  } catch (err) {
    console.error('Failed to load orders flag', err)
  }
})

const companyName = ref('')
const email = ref('')
const slug = ref('')
// Example address in the hint: <wish>.<base domain>, without a base domain the path /status/<wish>.
const slugHint = computed(() => {
  const base = primaryBaseDomain()
  const example = t('order.slugExample')
  return t('order.slugHint', { address: base ? `${example}.${base}` : `/status/${example}` })
})
// Honeypot: real users never see/fill this field (hidden via CSS
// + tabindex="-1"). A filled value makes the backend silently discard the
// order without giving the bot a detection signal
// - the success message stays identical.
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
    errorMessage.value = serverInputErrorText(err, t) || (err as Error).message
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

          <!-- Honeypot: invisible to real users via CSS, but present in the DOM -->
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
