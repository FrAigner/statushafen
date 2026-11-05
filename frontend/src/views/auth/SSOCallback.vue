<script setup>
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuth } from '../../composables/useAuth'
import { useApi } from '../../composables/useApi'

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
    const result = await apiFetch('/api/auth/sso-exchange', {
      method: 'POST',
      body: JSON.stringify({ code })
    })
    loginWithToken(result.token)
    // nach dem Verknüpfen zurück ins Profil. Nur dieser feste
    // Wert zählt, nie ein Ziel aus der URL (kein Open Redirect).
    router.push(params.get('next') === 'profile' ? '/admin/profile' : '/admin')
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
