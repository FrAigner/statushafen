<script setup lang="ts">
/*
  "Sign in with SSO" button for the OIDC page SSO.
  GET /api/public/pages/:slug/sso only reports whether OIDC is enabled for the page,
  plus the page ID used to start the login. If it is off or the lookup fails, the
  button stays hidden. Starting the login is a real browser navigation (redirect to
  the provider), hence a link instead of fetch.

  A private page reveals its page ID and SSO kind only to authorized users; for
  everyone else the lookup answers with 404, as for a missing page. With
  `fallback` the button then offers a link to the slug-based entry point
  (GET /api/auth/sso/login?slug=…), which the server resolves itself. It appears
  for a private and a missing page alike.

  The button sits in a wrapper element that receives passed attributes (class).

  After each lookup the component emits `available` (true when a button is
  shown), so the login page can tell whether any sign-in method is offered.
*/
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../composables/useApi'
import type { ApiError } from '../types/api'
import AppIcon from './AppIcon.vue'

/** Response of GET /api/public/pages/:slug/sso. */
interface PageSsoInfo {
  oidc_enabled?: boolean
  page_id?: number
}

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  slug?: string
  // On a 404 from the lookup, offer the slug-based entry point (private pages).
  fallback?: boolean
  // Internal path to come back to after the sign-in, sent as return_to (the backend checks it again).
  returnTo?: string
}>(), {
  slug: '',
  fallback: false,
  returnTo: '',
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineEmits out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ available: [available: boolean] }>()

const { t } = useI18n()
const { apiFetch } = useApi()
// Stryker disable next-line StringLiteral: equivalent initial value, load() resets it right away (watch immediate)
const loginUrl = ref('')

const load = async (slug: string) => {
  loginUrl.value = ''
  if (slug) await lookup(slug)
  emit('available', loginUrl.value !== '')
}

const lookup = async (slug: string) => {
  const base = import.meta.env.VITE_API_URL || ''
  const returnParam = props.returnTo ? `&return_to=${encodeURIComponent(props.returnTo)}` : ''
  try {
    const info: PageSsoInfo = await apiFetch(`/api/public/pages/${encodeURIComponent(slug)}/sso`)
    if (info.oidc_enabled) {
      loginUrl.value = `${base}/api/auth/oidc/login?page=${info.page_id}${returnParam}`
    }
  } catch (err) {
    // Private or missing page: the server resolves the slug-based entry point itself.
    if (props.fallback && (err as ApiError | undefined)?.status === 404) {
      loginUrl.value = `${base}/api/auth/sso/login?slug=${encodeURIComponent(slug)}${returnParam}`
    }
    // Otherwise no button: the other sign-in methods remain usable.
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
