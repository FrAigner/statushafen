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

  Dieselbe Abfrage meldet saml_enabled; dann erscheint ein zweiter
  Knopf für den SAML-Login. Beide liegen in einem gemeinsamen Element, das
  übergebene Attribute (class) erhält. Der Komponentenname bleibt, damit
  Login- und Statusseite unverändert einbinden.
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
})
// Stryker restore all

const { t } = useI18n()
const { apiFetch } = useApi()
// Stryker disable next-line StringLiteral: Startwert äquivalent, load() setzt ihn sofort zurück (watch immediate)
const loginUrl = ref('')
// Stryker disable next-line StringLiteral: Startwert äquivalent, load() setzt ihn sofort zurück (watch immediate)
const samlLoginUrl = ref('')

const load = async (slug) => {
  loginUrl.value = ''
  samlLoginUrl.value = ''
  if (!slug) return
  const base = import.meta.env.VITE_API_URL || ''
  try {
    const info = await apiFetch(`/api/public/pages/${encodeURIComponent(slug)}/sso`)
    if (info.oidc_enabled) {
      loginUrl.value = `${base}/api/auth/oidc/login?page=${info.page_id}`
    }
    if (info.saml_enabled) {
      samlLoginUrl.value = `${base}/api/auth/saml/login?page=${info.page_id}`
    }
  } catch (err) {
    // Private oder fehlende Seite: der Server löst den Einstieg über den Slug selbst auf.
    if (props.fallback && err?.status === 404) {
      loginUrl.value = `${base}/api/auth/sso/login?slug=${encodeURIComponent(slug)}`
    }
    // Sonst kein Knopf: Die übrigen Anmeldewege bleiben nutzbar.
  }
}

watch(() => props.slug, load, { immediate: true })
</script>

<template>
  <div v-if="loginUrl || samlLoginUrl" class="sso-login-buttons">
    <a v-if="loginUrl" class="oidc-login-btn" :href="loginUrl">
      <AppIcon name="shield" :size="18" />
      {{ t('auth.login.oidcButton') }}
    </a>
    <a v-if="samlLoginUrl" class="saml-login-btn" :href="samlLoginUrl">
      <AppIcon name="shield" :size="18" />
      {{ t('auth.login.samlButton') }}
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

.oidc-login-btn,
.saml-login-btn {
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

.oidc-login-btn:hover,
.saml-login-btn:hover {
  border-color: var(--primary);
}

.oidc-login-btn:focus-visible,
.saml-login-btn:focus-visible {
  outline: 2px solid var(--focus, var(--primary));
  outline-offset: 2px;
}
</style>
