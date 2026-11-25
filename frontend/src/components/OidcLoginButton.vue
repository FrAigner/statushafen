<script setup>
/*
  Knopf „Mit SSO anmelden“ für das OIDC-Seiten-SSO.
  GET /api/public/pages/:slug/sso meldet nur, ob OIDC für die Seite aktiv ist,
  und die Seiten-ID für den Login-Start. Ist es aus oder schlägt die Abfrage
  fehl, bleibt der Knopf unsichtbar. Der Login-Start ist eine echte
  Browser-Navigation (Redirect zum Anbieter), deshalb ein Link statt fetch.

  Eine private Seite nennt Seiten-ID und SSO-Art nur Berechtigten; für alle
  anderen antwortet die Abfrage wie bei einer fehlenden Seite mit 404. Mit
  `fallback` bietet der Knopf dann einen Link auf den Einstieg über den Slug an
  (GET /api/auth/sso/login?slug=…), den der Server selbst auflöst. Er erscheint
  für eine private und eine fehlende Seite gleichermaßen.

  The button sits in a wrapper element that receives passed attributes (class).

  After each lookup the component emits `available` (true when a button is
  shown), so the login page can tell whether any sign-in method is offered.
*/
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../composables/useApi'
import AppIcon from './AppIcon.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  slug: { type: String, default: '' },
  // Bei 404 der Auskunft den Einstieg über den Slug anbieten (private Seiten).
  fallback: { type: Boolean, default: false },
  // Internal path to come back to after the sign-in, sent as return_to (the backend checks it again).
  returnTo: { type: String, default: '' },
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineEmits out of setup, instrumented code must not reference local variables there
const emit = defineEmits(['available'])

const { t } = useI18n()
const { apiFetch } = useApi()
// Stryker disable next-line StringLiteral: Startwert äquivalent, load() setzt ihn sofort zurück (watch immediate)
const loginUrl = ref('')

const load = async (slug) => {
  loginUrl.value = ''
  if (slug) await lookup(slug)
  emit('available', loginUrl.value !== '')
}

const lookup = async (slug) => {
  const base = import.meta.env.VITE_API_URL || ''
  const returnParam = props.returnTo ? `&return_to=${encodeURIComponent(props.returnTo)}` : ''
  try {
    const info = await apiFetch(`/api/public/pages/${encodeURIComponent(slug)}/sso`)
    if (info.oidc_enabled) {
      loginUrl.value = `${base}/api/auth/oidc/login?page=${info.page_id}${returnParam}`
    }
  } catch (err) {
    // Private oder fehlende Seite: der Server löst den Einstieg über den Slug selbst auf.
    if (props.fallback && err?.status === 404) {
      loginUrl.value = `${base}/api/auth/sso/login?slug=${encodeURIComponent(slug)}${returnParam}`
    }
    // Sonst kein Knopf: Die übrigen Anmeldewege bleiben nutzbar.
  }
}

watch(() => props.slug, load, { immediate: true })
</script>

<template>
  <div v-if="loginUrl" class="sso-login-buttons">
    <a class="oidc-login-btn" :href="loginUrl">
      <AppIcon name="shield" :size="18" />
      {{ t('auth.login.oidcButton') }}
    </a>
  </div>
</template>

<style scoped>
.sso-login-buttons {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

.oidc-login-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  width: 100%;
  box-sizing: border-box;
  padding: 12px 18px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 12px;
  font-size: 0.95rem;
  font-weight: 500;
  color: var(--text);
  text-decoration: none;
}

.oidc-login-btn:hover {
  border-color: var(--primary);
}

.oidc-login-btn:focus-visible {
  outline: 2px solid var(--focus, var(--primary));
  outline-offset: 2px;
}
</style>
