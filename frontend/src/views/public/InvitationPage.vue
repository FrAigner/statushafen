<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { readInvitationToken, storeInvitationToken, clearInvitationToken } from '../../utils/invitationToken'
import AppIcon from '../../components/AppIcon.vue'
import '../../assets/entry.css'

// Einladung auf eine Statusseite annehmen oder ablehnen. Der Link
// aus der Einladungsmail führt hierher (/invitation#token=...). Mitglied wird
// das Konto erst mit dem Klick auf „Annehmen“, angemeldet mit der eingeladenen
// Adresse. Wie bei der Abo-Bestätigung kein automatischer Aufruf beim Laden,
// der den Token verbraucht (die Vorschau verbraucht ihn nicht).

// Token im URL-Fragment statt in der Query. Ein Fragment geht nie an
// den Server und nie im Referer hinaus (Caddy-/Cloudflare-/Proxy-Logs sehen ihn
// nicht); Browser-Verlauf und Adresszeile bekommen ihn nur bis zum Lesen.
// Reihenfolge: Fragment (neue Mails), alte Query (Übergang), sessionStorage
// (Neuladen/Login-Rückkehr im selben Tab).
//
// Reload-Entscheidung (konservativ): Sobald der Token gelesen ist, ersetzt die
// Seite die Adresse durch /invitation (ohne Query, ohne Fragment). Damit
// Neuladen und die Rückkehr vom Login nicht am fehlenden Token scheitern, liegt
// er bis zum Abschluss in sessionStorage (nur dieser Tab, weg beim Schließen,
// nie localStorage, nie in einer URL). Nach Annehmen, Ablehnen oder einem endgültigen Fehler wird
// er gelöscht. Ohne sessionStorage (blockiert) klickt man den Mail-Link erneut.
//
// Übergangszeit: Der Query-Fallback (?token=) dient nur den vor der Umstellung
// verschickten Mails. Einladungen gelten 7 Tage
// (pageInvitationTTL in backend/handlers/page_invitation.go): Der Fallback kann
// entfernt werden nach 2026-11-11 (Stand 2026-11-04 + 7 Tage), gerechnet ab
// dem Deployment-Datum, falls es später liegt.
// Speicherzugriff geteilt mit useAuth.logout (utils/invitationToken.js).

// Endgültige Fehler: Der Token taugt nie wieder, dann wird er vergessen. Bei
// allen anderen (500, Netzwerk, Kontokonflikt, Limit) bleibt er, damit Neuladen
// nach der schon bereinigten Adresse weiter funktioniert.
const FINAL_ERROR_CODES = ['invitation_not_found', 'invitation_expired', 'invitation_already_member']

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { apiFetch } = useApi()
const { isLoggedIn } = useAuth()

// state: 'login' | 'loading' | 'ready' | 'busy' | 'accepted' | 'declined' | 'error'
const state = ref('loading')
// Stryker disable next-line StringLiteral: wird vor jeder Anzeige gesetzt
const errorMessage = ref('')
const invitation = ref(null)
// Stryker disable next-line StringLiteral: ohne Token-Query bleibt die Seite im Fehlerzustand
const token = ref('')

// Der Token steht weder in returnTo noch sonst in der Login-Adresse; nach dem Login
// liest die Seite ihn aus sessionStorage (Open-Redirect-Schutz: nur interner Pfad).
const loginLink = computed(() => '/login?returnTo=' + encodeURIComponent('/invitation'))

function fail(err) {
  if (FINAL_ERROR_CODES.includes(err.data?.code)) clearInvitationToken()
  errorMessage.value = err.message
  // Stryker disable next-line StringLiteral: jeder unbekannte Zustand fällt in den v-else-Zweig (Fehleranzeige)
  state.value = 'error'
}

async function post(path) {
  return apiFetch(path, { method: 'POST', body: JSON.stringify({ token: token.value }) })
}

onMounted(async () => {
  // Zustand VOR dem await festhalten: erst lesen, dann Adresse bereinigen.
  const hashToken = new URLSearchParams(route.hash.slice(1)).get('token')
  const urlToken = hashToken || route.query.token
  const found = urlToken ? String(urlToken) : readInvitationToken()
  if (!found) {
    errorMessage.value = t('invitation.noTokenError')
    // Stryker disable next-line StringLiteral: jeder unbekannte Zustand fällt in den v-else-Zweig (Fehleranzeige)
    state.value = 'error'
    return
  }
  token.value = found
  if (urlToken) {
    storeInvitationToken(found)
    // Stryker disable next-line ObjectLiteral: replace({}) löst in vue-router 4 auf den aktuellen Pfad ohne Query/Fragment auf, gleiche Wirkung
    await router.replace({ path: route.path })
  }
  if (!isLoggedIn.value) {
    state.value = 'login'
    return
  }
  try {
    invitation.value = await post('/api/invitations/preview')
    state.value = 'ready'
  } catch (err) {
    fail(err)
  }
})

async function respond(action) {
  state.value = 'busy'
  try {
    await post(`/api/invitations/${action}`)
    clearInvitationToken()
    state.value = action === 'accept' ? 'accepted' : 'declined'
  } catch (err) {
    fail(err)
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
