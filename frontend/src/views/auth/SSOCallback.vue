<script setup>
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuth } from '../../composables/useAuth'
import { useApi } from '../../composables/useApi'
import { safeReturnTo } from '../../utils/safeReturnTo'

const router = useRouter()
const { t } = useI18n()
const { loginWithToken } = useAuth()
const { apiFetch } = useApi()

onMounted(async () => {
  const params = new URLSearchParams(window.location.search)
  const code = params.get('code')

  if (!code) {
    router.push('/login?error=sso_failed')
    return
  }

  try {
    // The backend redeems the code only with the browser binding cookie set at
    // the login start (login CSRF); send it also to an API on another origin.
    const result = await apiFetch('/api/auth/sso-exchange', {
      method: 'POST',
      body: JSON.stringify({ code }),
      credentials: 'include'
    })
    loginWithToken(result.token)
    // After linking go back to the profile (fixed value, never a target from the URL).
    // Otherwise return_to from the exchange answer, which the backend kept from the
    // login start; it is checked like any returnTo, falling back to the landing page.
    router.push(params.get('next') === 'profile' ? '/admin/profile' : safeReturnTo(result.return_to, router, '/'))
  } catch (err) {
    console.error('SSO-Code-Austausch fehlgeschlagen:', err)
    router.push('/login?error=sso_failed')
  }
})
</script>

<template>
  <div class="sso-callback">
    <div class="sso-callback-inner">
      <div class="sso-spinner" role="status" :aria-label="t('auth.sso.loadingAria')"></div>
      <p>{{ t('auth.sso.processing') }}</p>
    </div>
  </div>
</template>

<style scoped>
.sso-callback {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  color: var(--text);
}

.sso-callback-inner {
  text-align: center;
}

.sso-spinner {
  width: 32px;
  height: 32px;
  margin: 0 auto 16px;
  border-radius: 50%;
  border: 3px solid var(--border, var(--border));
  border-top-color: var(--primary);
  animation: sso-spin 0.7s linear infinite;
}

@keyframes sso-spin {
  to { transform: rotate(360deg); }
}
</style>
