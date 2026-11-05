<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import '../../assets/entry.css'

const route = useRoute()
const { t } = useI18n()
const { apiFetch } = useApi()

// state: 'idle' (wartet auf Klick) | 'confirming' | 'success' | 'error'
const state = ref('idle')
const errorMessage = ref('')
const setPasswordToken = ref('')
const token = ref('')

onMounted(() => {
  const queryToken = route.query.token
  if (!queryToken) {
    state.value = 'error'
    errorMessage.value = t('order.missingTokenError')
    return
  }
  token.value = queryToken
})

// Bewusst kein Auto-Fire beim Mounten: manche Mail-Gateways prefetchen Links
// automatisch und würden den Einmal-Token sonst schon vor dem eigentlichen
// Klick der Nutzerin/des Nutzers verbrauchen.
async function confirm() {
  state.value = 'confirming'
  try {
    const data = await apiFetch('/api/public/orders/confirm', {
      method: 'POST',
      body: JSON.stringify({ token: token.value }),
    })
    setPasswordToken.value = data.set_password_token
    state.value = 'success'
  } catch (err) {
    errorMessage.value = err.message
    state.value = 'error'
  }
}
</script>

<template>
  <div class="order-confirm-page entry-page">
    <div class="order-confirm-card entry-card entry-card--wide entry-card--center">
      <div v-if="state === 'idle' || state === 'confirming'">
        <h1 class="entry-title">{{ t('order.confirmPageTitle') }}</h1>
        <p class="entry-subtitle">{{ t('order.confirmPageSubtitle') }}</p>
        <button class="btn-confirm entry-btn" :disabled="state === 'confirming'" @click="confirm">
          {{ t('order.confirmButton') }}
        </button>
      </div>
      <div v-else-if="state === 'success'" class="order-confirm-success">
        <h1 class="entry-title">{{ t('order.confirmSuccessTitle') }}</h1>
        <p class="entry-subtitle">{{ t('order.confirmSuccessMessage') }}</p>
        <router-link :to="`/order/set-password?token=${setPasswordToken}`" class="btn-confirm entry-btn">
          {{ t('order.confirmSetPasswordButton') }}
        </router-link>
      </div>
      <div v-else class="order-confirm-error entry-alert" role="alert">
        <p>{{ errorMessage }}</p>
      </div>
    </div>
  </div>
</template>
