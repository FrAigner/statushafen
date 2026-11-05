<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import AppIcon from '../../components/AppIcon.vue'
import '../../assets/entry.css'

const route = useRoute()
const { apiFetch } = useApi()
const { t } = useI18n()

// Abbestellen erst nach Klick per POST, wie die
// Double-Opt-in-Seite (SubscribeConfirmPage.vue). Vorher löste schon das
// Öffnen des Links (GET beim Mounten) das Löschen aus - Link-Scanner und
// Prefetcher von Mail-Gateways beendeten so Abos ungewollt.
// status: 'idle' (wartet auf Klick) | 'loading' | 'success' | 'error'
const status = ref('idle')
// Stryker disable next-line StringLiteral: wird vor dem Fehlerzustand immer gesetzt
const errorMessage = ref('')
// Stryker disable next-line StringLiteral: ohne Token-Query bleibt die Seite im Fehlerzustand
const token = ref('')

onMounted(() => {
  const queryToken = route.query.token
  if (!queryToken) {
    status.value = 'error'
    errorMessage.value = t('unsubscribe.noTokenError')
    return
  }
  token.value = queryToken
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
    errorMessage.value = err.message
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
