<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { OnyxButton } from 'sit-onyx'

import { useAuth } from '../../composables/useAuth'
import { useApi } from '../../composables/useApi'
import { useAuthConfig } from '../../composables/useAuthConfig'
import { safeReturnTo } from '../../utils/safeReturnTo'
import type { ApiError } from '../../types/api'
import AppIcon from '../../components/AppIcon.vue'
import OidcLoginButton from '../../components/OidcLoginButton.vue'
import '../../assets/entry.css'

const router = useRouter()
const { t, te } = useI18n()
const { setAuth } = useAuth()
const { apiFetch } = useApi()

/** Response of POST /api/login: the token plus the profile for setAuth. */
interface LoginResponse {
  token: string
  id: number
  email: string
  role: string
  permissions: string
  tag_ids?: number[]
  is_global_admin?: boolean
  preferred_language?: string
  help_tooltips?: boolean
}
// Allowed sign-in methods come from the backend; without an answer there are no buttons.
const { authConfig, loadFailed: authConfigFailed, loadAuthConfig } = useAuthConfig()
const providers = computed(() => authConfig.value?.providers ?? [])
// Unknown config (loading or failed) never shows the password form.
const passwordLogin = computed(() => authConfig.value?.passwordLogin === true)
onMounted(loadAuthConfig)
// Page SSO: null until OidcLoginButton has answered, then whether it shows a button.
const pageSsoAvailable = ref<boolean | null>(null)
const hasSsoButton = computed(() => providers.value.length > 0 || pageSsoAvailable.value === true)
// Only claim "no method" once everything is known: config loaded and page SSO answered.
const noLoginMethod = computed(() =>
  authConfig.value !== null && !passwordLogin.value && providers.value.length === 0
  && (!pageSlug || pageSsoAvailable.value === false))

const loginEmail = ref('')
const loginPass = ref('')
const showPassword = ref(false)
const errorMessage = ref('')
const isSubmitting = ref(false)
const urlParams = new URLSearchParams(window.location.search)
const urlError = ref(urlParams.get('error'))
// Page SSO. ?page=<slug> comes from the status page (header button or
// private error state), ?oidc_error=<code> from the OIDC callback.
// Only known codes are translated; the raw value is never shown.
const pageSlug = urlParams.get('page')
// returnTo is attacker input: only checked internal paths, otherwise the landing page.
// The same target goes to the password login and, as return_to, to every OIDC start.
const returnTo = safeReturnTo(urlParams.get('returnTo'), router, '/')
const oidcErrorCode = urlParams.get('oidc_error')
const oidcErrorMessage = computed(() => {
  if (!oidcErrorCode) return ''
  const key = `auth.oidcErrors.${oidcErrorCode}`
  return te(key) ? t(key) : t('auth.oidcErrors.unknown')
})

// Global OIDC provider: real browser navigation, the server redirects to the provider.
const startProviderLogin = (id: string) => {
  const apiBase = import.meta.env.VITE_API_URL || ''
  window.location.assign(`${apiBase}/api/auth/provider/${encodeURIComponent(id)}/login?return_to=${encodeURIComponent(returnTo)}`)
}

// 401 of the app means wrong credentials (not Caddy's Basic Auth 401); 403
// password_login_disabled means the backend switched passwords off meanwhile.
const loginErrorMessage = (err: ApiError) => {
  if (err.status === 401 && !err.basicAuth) return t('auth.login.invalidCredentials')
  if (err.status === 403 && err.data?.code === 'password_login_disabled') return t('api.errors.password_login_disabled')
  return t('auth.login.failed', { message: err.message })
}

const doLogin = async (email: string, password: string) => {
  // A double submit (double click, Enter) would otherwise send two login requests.
  if (isSubmitting.value) return
  isSubmitting.value = true
  errorMessage.value = ''
  try {
    // auth: false - a still stored token does not belong to the
    // login, and its 401 would otherwise look like a session end.
    const data = await apiFetch<LoginResponse>('/api/login', {
      method: 'POST',
      auth: false,
      body: JSON.stringify({ email, password })
    })
    setAuth(data.token, {
      // id, so the user management recognizes the own row.
      id: data.id,
      email: data.email,
      role: data.role,
      permissions: data.permissions,
      tag_ids: data.tag_ids,
      is_global_admin: data.is_global_admin,
      preferred_language: data.preferred_language,
      help_tooltips: data.help_tooltips
    })
  } catch (err) {
    errorMessage.value = loginErrorMessage(err as ApiError)
    isSubmitting.value = false
    return
  }
  try {
    // Await the navigation, otherwise the button is released while the lazy chunk is still loading.
    await router.push(returnTo)
  } catch {
    // The login succeeded, only the navigation failed (e.g. a stale chunk after a deploy):
    // no login error message; reload the page with fresh assets instead.
    window.location.assign(returnTo)
  }
  isSubmitting.value = false
}
</script>

<template>
  <div class="login-page entry-page">
    <div class="login-card entry-card">
      <!-- Logo & title -->
      <div class="login-header entry-head">
        <div class="login-logo entry-logo"><AppIcon name="zap" :size="32" /></div>
        <h1 class="login-title entry-title">Statushafen</h1>
        <p class="login-subtitle entry-subtitle">{{ t('auth.login.subtitle') }}</p>
      </div>

      <!-- Error messages -->
      <div v-if="errorMessage" class="login-alert error entry-alert" role="alert">{{ errorMessage }}</div>
      <div v-if="urlError === 'sso_failed'" class="login-alert error entry-alert" role="alert">
        {{ t('auth.login.ssoFailed') }}
      </div>
      <div v-if="oidcErrorMessage" class="login-alert error entry-alert" role="alert">{{ oidcErrorMessage }}</div>
      <div v-if="authConfigFailed" class="login-alert error entry-alert" role="alert" data-test="auth-config-error">
        {{ t('auth.login.configLoadError') }}
      </div>

      <!-- SSO Buttons -->
      <div class="sso-section entry-actions">
        <OidcLoginButton v-if="pageSlug" :slug="pageSlug" :return-to="returnTo" fallback @available="pageSsoAvailable = $event" />
        <OnyxButton
          v-for="p in providers"
          :key="p.id"
          class="provider-login-btn"
          type="button"
          mode="outline"
          :label="t('auth.login.providerButton', { name: p.name })"
          @click="startProviderLogin(p.id)"
        />
      </div>

      <div v-if="noLoginMethod" class="login-alert entry-alert" role="status" data-test="no-login-method">
        {{ t('auth.login.noMethod') }}
      </div>

      <!-- Divider only between sign-in buttons and the password form -->
      <div v-if="passwordLogin && hasSsoButton" class="divider entry-divider">
        <span>{{ t('auth.login.orDivider') }}</span>
      </div>

      <!-- Email/password form, only when the backend allows it -->
      <form v-if="passwordLogin" novalidate @submit.prevent="doLogin(loginEmail, loginPass)" class="login-form entry-form">
        <p class="entry-hint" data-test="required-hint">{{ t('patterns.requiredHint') }}</p>
        <div class="entry-field">
          <label class="onyx-required-marker" for="login-email">{{ t('auth.login.emailLabel') }}</label>
          <input id="login-email" type="email" v-model="loginEmail" required aria-required="true" :placeholder="t('auth.login.emailPlaceholder')" autocomplete="email" />
        </div>
        <div class="entry-field">
          <label class="onyx-required-marker" for="login-password">{{ t('auth.login.passwordLabel') }}</label>
          <div class="password-input-wrap entry-password">
            <input
              id="login-password"
              :type="showPassword ? 'text' : 'password'"
              v-model="loginPass"
              required
              aria-required="true"
              placeholder="••••••••"
              autocomplete="current-password"
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
        </div>
        <button class="btn-login entry-btn" type="submit" :disabled="isSubmitting">
          {{ t('auth.login.submit') }} <AppIcon name="arrow-right" :size="16" />
        </button>
      </form>
    </div>
  </div>
</template>
