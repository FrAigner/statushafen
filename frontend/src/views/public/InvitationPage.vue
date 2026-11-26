<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { readInvitationToken, storeInvitationToken, clearInvitationToken } from '../../utils/invitationToken'
import AppIcon from '../../components/AppIcon.vue'
import type { ApiError } from '../../types/api'
import '../../assets/entry.css'

// Accept or decline an invitation to a status page. The link
// in the invitation mail leads here (/invitation#token=...). The account only
// becomes a member with the click on "Accept", signed in with the invited
// address. As with the subscription confirmation there is no automatic call on load
// that uses up the token (the preview does not use it up).

// Token in the URL fragment instead of the query. A fragment never goes to
// the server and never out in the Referer (Caddy/Cloudflare/proxy logs do not
// see it); browser history and address bar only get it until it is read.
// Order: fragment (new mails), old query (transition), sessionStorage
// (reload/login return in the same tab).
//
// Reload decision (conservative): as soon as the token is read, the page
// replaces the address with /invitation (no query, no fragment). So that
// reloading and returning from the login do not fail on the missing token, it is
// kept in sessionStorage until completion (only this tab, gone on close,
// never localStorage, never in a URL). After accepting, declining or a final error it
// is deleted. Without sessionStorage (blocked) the user clicks the mail link again.
//
// Transition period: the query fallback (?token=) only serves mails sent before
// the switch. Invitations are valid for 7 days
// (pageInvitationTTL in backend/handlers/page_invitation.go): the fallback can
// be removed after 2026-11-11 (as of 2026-11-04 + 7 days), counted from
// the deployment date if that is later.
// Storage access shared with useAuth.logout (utils/invitationToken.js).

// Final errors: the token will never work again, so it is forgotten. For
// all others (500, network, account conflict, limit) it stays, so that reloading
// after the already cleaned address keeps working.
const FINAL_ERROR_CODES = ['invitation_not_found', 'invitation_expired', 'invitation_already_member']

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { apiFetch } = useApi()
const { isLoggedIn } = useAuth()

/** Response of POST /api/invitations/preview. */
interface InvitationPreview {
  page_name?: string
  role_name?: string
}

// state: 'login' | 'loading' | 'ready' | 'busy' | 'accepted' | 'declined' | 'error'
const state = ref<'login' | 'loading' | 'ready' | 'busy' | 'accepted' | 'declined' | 'error'>('loading')
// Stryker disable next-line StringLiteral: set before every display
const errorMessage = ref('')
const invitation = ref<InvitationPreview | null>(null)
// Stryker disable next-line StringLiteral: without a token query the page stays in the error state
const token = ref('')

// The token is neither in returnTo nor anywhere else in the login address; after the login
// the page reads it from sessionStorage (open redirect protection: internal path only).
const loginLink = computed(() => '/login?returnTo=' + encodeURIComponent('/invitation'))

function fail(err: ApiError) {
  if (FINAL_ERROR_CODES.includes(err.data?.code as string)) clearInvitationToken()
  errorMessage.value = err.message
  // Stryker disable next-line StringLiteral: every unknown state falls into the v-else branch (error display)
  state.value = 'error'
}

async function post<T = unknown>(path: string) {
  return apiFetch<T>(path, { method: 'POST', body: JSON.stringify({ token: token.value }) })
}

onMounted(async () => {
  // Capture the state BEFORE the await: read first, then clean the address.
  const hashToken = new URLSearchParams(route.hash.slice(1)).get('token')
  const urlToken = hashToken || route.query.token
  const found = urlToken ? String(urlToken) : readInvitationToken()
  if (!found) {
    errorMessage.value = t('invitation.noTokenError')
    // Stryker disable next-line StringLiteral: every unknown state falls into the v-else branch (error display)
    state.value = 'error'
    return
  }
  token.value = found
  if (urlToken) {
    storeInvitationToken(found)
    // Stryker disable next-line ObjectLiteral: in vue-router 4 replace({}) resolves to the current path without query/fragment, same effect
    await router.replace({ path: route.path })
  }
  if (!isLoggedIn.value) {
    state.value = 'login'
    return
  }
  try {
    invitation.value = await post<InvitationPreview>('/api/invitations/preview')
    state.value = 'ready'
  } catch (err) {
    fail(err as ApiError)
  }
})

async function respond(action: 'accept' | 'decline') {
  state.value = 'busy'
  try {
    await post(`/api/invitations/${action}`)
    clearInvitationToken()
    state.value = action === 'accept' ? 'accepted' : 'declined'
  } catch (err) {
    fail(err as ApiError)
  }
}
</script>

<template>
  <div class="invitation-page entry-page">
    <div class="entry-card entry-card--wide entry-card--center">
      <div v-if="state === 'login'" class="invitation-login">
        <span class="entry-icon"><AppIcon name="mail" :size="28" /></span>
        <h1 class="entry-title">{{ t('invitation.title') }}</h1>
        <p class="entry-subtitle">{{ t('invitation.loginHint') }}</p>
        <router-link :to="loginLink" class="entry-btn">{{ t('invitation.loginButton') }}</router-link>
      </div>
      <div v-else-if="state === 'loading'" class="invitation-loading">
        <p class="entry-subtitle" role="status">{{ t('invitation.loading') }}</p>
      </div>
      <div v-else-if="state === 'ready' || state === 'busy'" class="invitation-ready">
        <span class="entry-icon"><AppIcon name="users" :size="28" /></span>
        <h1 class="entry-title">{{ t('invitation.title') }}</h1>
        <p class="entry-subtitle">{{ t('invitation.subtitle', { page: invitation?.page_name, role: invitation?.role_name }) }}</p>
        <div class="entry-actions">
          <button type="button" class="entry-btn btn-accept" :disabled="state === 'busy'" @click="respond('accept')">
            {{ t('invitation.accept') }}
          </button>
          <button type="button" class="entry-btn entry-btn--secondary btn-decline" :disabled="state === 'busy'" @click="respond('decline')">
            {{ t('invitation.decline') }}
          </button>
        </div>
      </div>
      <div v-else-if="state === 'accepted'" class="invitation-accepted">
        <span class="entry-icon"><AppIcon name="check-circle" :size="28" /></span>
        <h1 class="entry-title">{{ t('invitation.acceptedTitle') }}</h1>
        <p class="entry-subtitle">{{ t('invitation.acceptedMessage', { page: invitation?.page_name }) }}</p>
        <router-link to="/admin" class="entry-btn">{{ t('invitation.toAdmin') }}</router-link>
      </div>
      <div v-else-if="state === 'declined'" class="invitation-declined">
        <span class="entry-icon"><AppIcon name="check-circle" :size="28" /></span>
        <h1 class="entry-title">{{ t('invitation.declinedTitle') }}</h1>
        <p class="entry-subtitle">{{ t('invitation.declinedMessage') }}</p>
      </div>
      <div v-else class="invitation-error">
        <span class="entry-icon entry-icon--danger"><AppIcon name="alert-triangle" :size="28" /></span>
        <h1 class="entry-title">{{ t('invitation.errorTitle') }}</h1>
        <p class="entry-alert" role="alert">{{ errorMessage }}</p>
      </div>
    </div>
  </div>
</template>
