<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import AppIcon from '../../components/AppIcon.vue'
import '../../assets/entry.css'

const route = useRoute()
const { apiFetch } = useApi()
const { t } = useI18n()

// Unsubscribe only after a click, via POST, like the
// double opt-in page (SubscribeConfirmPage.vue). Previously merely opening
// the link (GET on mount) triggered the deletion - link scanners and
// prefetchers of mail gateways ended subscriptions unintentionally.
// status: 'idle' (waiting for a click) | 'loading' | 'success' | 'error'
const status = ref<'idle' | 'loading' | 'success' | 'error'>('idle')
// Stryker disable next-line StringLiteral: always set before the error state
const errorMessage = ref('')
// Stryker disable next-line StringLiteral: without a token query the page stays in the error state
const token = ref('')

onMounted(() => {
  const queryToken = route.query.token
  if (!queryToken) {
    status.value = 'error'
    errorMessage.value = t('unsubscribe.noTokenError')
    return
  }
  token.value = queryToken as string
})

async function unsubscribe() {
  status.value = 'loading'
  try {
    await apiFetch('/api/public/subscribers/unsubscribe', {
      method: 'POST',
      body: JSON.stringify({ token: token.value }),
    })
    status.value = 'success'
  } catch (err) {
    status.value = 'error'
    errorMessage.value = (err as Error).message
  }
}
</script>

<template>
  <div class="unsubscribe-page entry-page">
    <div class="unsubscribe-card entry-card entry-card--center">
      <div v-if="status === 'idle' || status === 'loading'">
        <span class="entry-icon"><AppIcon name="bell" :size="28" /></span>
        <h1 class="entry-title">{{ t('unsubscribe.title') }}</h1>
        <p class="entry-subtitle">{{ t('unsubscribe.subtitle') }}</p>
        <button class="btn-unsubscribe entry-btn" :disabled="status === 'loading'" @click="unsubscribe">
          {{ status === 'loading' ? t('unsubscribe.loading') : t('unsubscribe.button') }}
        </button>
      </div>
      <div v-else-if="status === 'success'">
        <span class="entry-icon"><AppIcon name="check-circle" :size="28" /></span>
        <h1 class="entry-title">{{ t('unsubscribe.successTitle') }}</h1>
        <p class="entry-subtitle">{{ t('unsubscribe.successMessage') }}</p>
      </div>
      <div v-else>
        <span class="entry-icon entry-icon--danger"><AppIcon name="alert-triangle" :size="28" /></span>
        <h1 class="entry-title">{{ t('unsubscribe.errorTitle') }}</h1>
        <p class="entry-alert" role="alert">{{ errorMessage }}</p>
      </div>
    </div>
  </div>
</template>
