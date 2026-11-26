<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import AppIcon from '../../components/AppIcon.vue'
import '../../assets/entry.css'

// Confirmation page of the double opt-in for email subscriptions. The link
// in the confirmation mail leads here (/subscribe/confirm?token=...).

const route = useRoute()
const { t } = useI18n()
const { apiFetch } = useApi()

// state: 'idle' (waiting for a click) | 'confirming' | 'success' | 'error'
const state = ref<'idle' | 'confirming' | 'success' | 'error'>('idle')
// The initial values are equivalent: every state that shows or sends them sets them first.
// Stryker disable next-line StringLiteral: always set before the error state
const errorMessage = ref('')
// Stryker disable next-line StringLiteral: always set before the success state
const slug = ref('')
// Stryker disable next-line StringLiteral: without a token query the page stays in the error state
const token = ref('')

onMounted(() => {
  const queryToken = route.query.token
  if (!queryToken) {
    state.value = 'error'
    errorMessage.value = t('subscribeConfirm.noTokenError')
    return
  }
  token.value = queryToken as string
})

// Deliberately no auto-fire on mount (like OrderConfirmPage): mail gateways
// with link prefetching would otherwise use up the one-time token before the click.
async function confirm() {
  state.value = 'confirming'
  try {
    const data = await apiFetch<{ slug?: string }>('/api/public/subscribers/confirm', {
      method: 'POST',
      body: JSON.stringify({ token: token.value }),
    })
    slug.value = data.slug || ''
    state.value = 'success'
  } catch (err) {
    errorMessage.value = (err as Error).message
    state.value = 'error'
  }
}
</script>

<template>
  <div class="subscribe-confirm-page entry-page">
    <div class="subscribe-confirm-card entry-card entry-card--wide entry-card--center">
      <div v-if="state === 'idle' || state === 'confirming'">
        <span class="entry-icon"><AppIcon name="bell" :size="28" /></span>
        <h1 class="entry-title">{{ t('subscribeConfirm.title') }}</h1>
        <p class="entry-subtitle">{{ t('subscribeConfirm.subtitle') }}</p>
        <button class="btn-confirm entry-btn" :disabled="state === 'confirming'" @click="confirm">
          {{ t('subscribeConfirm.button') }}
        </button>
      </div>
      <div v-else-if="state === 'success'" class="subscribe-confirm-success">
        <span class="entry-icon"><AppIcon name="check-circle" :size="28" /></span>
        <h1 class="entry-title">{{ t('subscribeConfirm.successTitle') }}</h1>
        <p class="entry-subtitle">{{ t('subscribeConfirm.successMessage') }}</p>
        <router-link v-if="slug" :to="`/status/${slug}`" class="btn-confirm entry-btn">
          {{ t('subscribeConfirm.toStatusPage') }}
        </router-link>
      </div>
      <div v-else class="subscribe-confirm-error">
        <span class="entry-icon entry-icon--danger"><AppIcon name="alert-triangle" :size="28" /></span>
        <h1 class="entry-title">{{ t('subscribeConfirm.errorTitle') }}</h1>
        <p class="entry-alert" role="alert">{{ errorMessage }}</p>
      </div>
    </div>
  </div>
</template>
