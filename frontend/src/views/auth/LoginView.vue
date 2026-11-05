<script setup>
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

import { useAuth } from '../../composables/useAuth'
import { useApi } from '../../composables/useApi'
import { safeReturnTo } from '../../utils/safeReturnTo'
import AppIcon from '../../components/AppIcon.vue'
import OidcLoginButton from '../../components/OidcLoginButton.vue'
import '../../assets/entry.css'

const router = useRouter()
const { t, te } = useI18n()
const { setAuth } = useAuth()
const { apiFetch } = useApi()

const loginEmail = ref('')
const loginPass = ref('')
const showPassword = ref(false)
const errorMessage = ref('')
const isSubmitting = ref(false)
const urlParams = new URLSearchParams(window.location.search)
const urlError = ref(urlParams.get('error'))
// Seiten-SSO. ?page=<slug> kommt von der Statusseite (Knopf im
// Kopf bzw. privater Fehlerzustand), ?oidc_error=<code> vom OIDC-Callback.
// Nur bekannte Codes werden übersetzt, nie der rohe Wert angezeigt.
const pageSlug = urlParams.get('page')
const oidcErrorCode = urlParams.get('oidc_error')
const oidcErrorMessage = computed(() => {
  if (!oidcErrorCode) return ''
  const key = `auth.oidcErrors.${oidcErrorCode}`
  return te(key) ? t(key) : t('auth.oidcErrors.unknown')
})

// SSO-Provider-URLs
const ssoProviders = computed(() => [
  { key: 'google', label: t('auth.login.googleButton'), icon: 'google' },
  { key: 'github', label: t('auth.login.githubButton'), icon: 'github' },
])

const doLogin = async (email, password) => {
  // Doppel-Submit (Doppelklick, Enter) sendet sonst zwei Login-Requests.
  if (isSubmitting.value) return
  isSubmitting.value = true
  errorMessage.value = ''
  try {
    // auth: false – ein noch gespeichertes Token gehört nicht zum
    // Login, und seine 401 wäre sonst ein vermeintliches Sitzungsende.
    const data = await apiFetch('/api/login', {
      method: 'POST',
      auth: false,
      body: JSON.stringify({ email, password })
    })
    setAuth(data.token, {
      // id, damit die Benutzerverwaltung die eigene Zeile erkennt.
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
    // Eine 401 der App heißt hier „Zugangsdaten falsch“ (übersetzt
    // statt der englischen Server-Meldung); Caddys Basic-Auth-401 nicht.
    errorMessage.value = err.status === 401 && !err.basicAuth
      ? t('auth.login.invalidCredentials')
      : t('auth.login.failed', { message: err.message })
    isSubmitting.value = false
    return
  }
  // AdminLayout führt zum ersten lesbaren Tab.
  // returnTo ist Angreifer-Eingabe, nur geprüfte interne Pfade (sonst /admin).
  const returnTo = safeReturnTo(new URLSearchParams(window.location.search).get('returnTo'), router, '/admin')
  try {
    // Navigation abwarten, sonst gibt der Button frei, solange der lazy Chunk noch lädt.
    await router.push(returnTo)
  } catch {
    // Login war erfolgreich, nur die Navigation scheiterte (z. B. veralteter Chunk nach einem Deploy):
    // keine Login-Fehlermeldung, stattdessen die Seite mit frischen Assets neu laden.
    window.location.assign(returnTo)
  }
  isSubmitting.value = false
}

const doSSOLogin = (provider) => {
  const apiBase = import.meta.env.VITE_API_URL || ''
  window.location.href = `${apiBase}/api/auth/${provider}`
}
</script>

<template>
  <div class="login-page entry-page">
    <div class="login-card entry-card">
      <!-- Logo & Titel -->
      <div class="login-header entry-head">
        <div class="login-logo entry-logo"><AppIcon name="zap" :size="32" /></div>
        <h1 class="login-title entry-title">Statushafen</h1>
        <p class="login-subtitle entry-subtitle">{{ t('auth.login.subtitle') }}</p>
      </div>

      <!-- Fehlermeldungen -->
      <div v-if="errorMessage" class="login-alert error entry-alert" role="alert">{{ errorMessage }}</div>
      <div v-if="urlError === 'sso_failed'" class="login-alert error entry-alert" role="alert">
        {{ t('auth.login.ssoFailed') }}
      </div>
      <div v-if="oidcErrorMessage" class="login-alert error entry-alert" role="alert">{{ oidcErrorMessage }}</div>

      <!-- SSO Buttons -->
      <div class="sso-section entry-actions">
        <OidcLoginButton v-if="pageSlug" :slug="pageSlug" fallback />
        <button
          v-for="p in ssoProviders"
          :key="p.key"
          class="sso-btn entry-btn entry-btn--secondary"
          @click="doSSOLogin(p.key)"
        >
          <span class="sso-icon"><AppIcon :name="p.icon" :size="18" /></span>
          {{ p.label }}
        </button>
      </div>

      <!-- Trennlinie -->
      <div class="divider entry-divider">
        <span>{{ t('auth.login.orDivider') }}</span>
      </div>

      <!-- Klassisches Login-Formular -->
      <form novalidate @submit.prevent="doLogin(loginEmail, loginPass)" class="login-form entry-form">
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
