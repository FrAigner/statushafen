<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import AppIcon from '../../components/AppIcon.vue'
import { MAX_PASSWORD_BYTES, MIN_PASSWORD_LENGTH, isPasswordTooLong, isPasswordTooShort, serverInputErrorText } from '../../utils/inputRules'
import '../../assets/entry.css'

const route = useRoute()
const { t } = useI18n()
const { apiFetch } = useApi()

const token = ref('')
const missingToken = ref(false)

const password = ref('')
const passwordConfirm = ref('')
const showPassword = ref(false)
const submitting = ref(false)
const submitted = ref(false)
const errorMessage = ref('')

onMounted(() => {
  const queryToken = route.query.token
  if (!queryToken) {
    missingToken.value = true
    return
  }
  token.value = queryToken
})

async function submitPassword() {
  errorMessage.value = ''
  if (password.value !== passwordConfirm.value) {
    errorMessage.value = t('order.passwordMismatch')
    return
  }
  // Mindestlänge vorab (das Backend prüft sie ebenfalls).
  if (isPasswordTooShort(password.value)) {
    errorMessage.value = t('inputErrors.password_too_short', { min: MIN_PASSWORD_LENGTH })
    return
  }
  if (isPasswordTooLong(password.value)) {
    errorMessage.value = t('inputErrors.password_too_long', { max: MAX_PASSWORD_BYTES })
    return
  }
  submitting.value = true
  try {
    await apiFetch('/api/public/orders/set-password', {
      method: 'POST',
      body: JSON.stringify({ token: token.value, password: password.value }),
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
  <div class="order-setpw-page entry-page">
    <div class="order-setpw-card entry-card">
      <div v-if="missingToken" class="order-setpw-error entry-alert" role="alert">
        <p>{{ t('order.missingSetPasswordTokenError') }}</p>
      </div>
      <div v-else-if="submitted" class="order-setpw-success">
        <h1 class="entry-title">{{ t('order.setPasswordSuccessTitle') }}</h1>
        <p class="entry-subtitle">{{ t('order.setPasswordSuccessMessage') }}</p>
        <router-link to="/login" class="btn-setpw-submit entry-btn">{{ t('order.goToLoginButton') }}</router-link>
      </div>
      <template v-else>
        <h1 class="entry-title">{{ t('order.setPasswordTitle') }}</h1>
        <p class="order-setpw-subtitle entry-subtitle">{{ t('order.setPasswordSubtitle') }}</p>

        <div v-if="errorMessage" class="order-setpw-form-error entry-alert" role="alert">{{ t('order.setPasswordErrorGeneric', { message: errorMessage }) }}</div>

        <form novalidate class="entry-form" @submit.prevent="submitPassword">
          <p class="entry-hint" data-test="required-hint">{{ t('patterns.requiredHint') }}</p>
          <div class="entry-field">
            <label class="onyx-required-marker" for="order-password">{{ t('order.passwordLabel') }}</label>
            <div class="password-input-wrap entry-password">
              <input
                id="order-password"
                :type="showPassword ? 'text' : 'password'"
                v-model="password"
                required
                aria-required="true"
                :minlength="MIN_PASSWORD_LENGTH"
                aria-describedby="order-password-hint"
                autocomplete="new-password"
              />
              <button
                type="button"
                class="password-toggle entry-toggle"
                :aria-label="showPassword ? t('auth.login.hidePassword') : t('auth.login.showPassword')"
                @click="showPassword = !showPassword"
              >
                <AppIcon :name="showPassword ? 'eye-off' : 'eye'" :size="16" />
              </button>
            </div>
            <p id="order-password-hint" class="entry-hint">{{ t('inputErrors.passwordHint', { min: MIN_PASSWORD_LENGTH }) }}</p>
          </div>
          <div class="entry-field">
            <label class="onyx-required-marker" for="order-password-confirm">{{ t('order.passwordConfirmLabel') }}</label>
            <input
              id="order-password-confirm"
              :type="showPassword ? 'text' : 'password'"
              v-model="passwordConfirm"
              required
              aria-required="true"
              autocomplete="new-password"
            />
          </div>
          <button class="btn-setpw-submit entry-btn" type="submit" :disabled="submitting">
            {{ t('order.setPasswordSubmit') }}
          </button>
        </form>
      </template>
    </div>
  </div>
</template>
