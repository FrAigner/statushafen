import { ref, computed } from 'vue'
import { useTenant } from './useTenant'
import i18n, { SUPPORTED_LOCALES, LANGUAGE_STORAGE_KEY, isLocaleLoaded, loadLocaleMessages } from '../i18n'
import { clearInvitationToken } from '../utils/invitationToken'
import { grants, isGlobalResource } from '../utils/permissions'

const token = ref(localStorage.getItem('jwt_token') || '')
const currentUser = ref(null)

export function useAuth() {
  const { activeTenantPermissions, setMyPages, setActiveTenant } = useTenant()

  // Wie das Backend (middleware.requirePermissionCheck) zählt nur
  // die Rolle auf der gerade aktiven Statusseite (UserStatusPageRole); die
  // globale Rolle im JWT (currentUser.permissions) wird nie ausgewertet, sonst
  // zeigt das UI Schreibknöpfe, die mit 403 enden. Global Admins bypassen
  // immer. create, update und delete einer Ressource schließen
  // deren read ein; der Altwert `:write` zählt als alle vier Aktionen
  // (Backend: permissions.Has). Globale Ressourcen (users, roles, tags, ...)
  // gehören nur Global Admins, auch wenn eine Seitenrolle den String trägt.
  const hasPerm = (required) => {
    if (currentUser.value?.is_global_admin) return true
    if (!currentUser.value) return false
    if (isGlobalResource(required.split(':')[0])) return false
    return grants(activeTenantPermissions.value, required)
  }

  // Übernimmt eine serverseitig gespeicherte Sprachpräferenz in
  // die aktive UI-Sprache, damit sie sich geräteübergreifend durchsetzt statt
  // der ggf. abweichenden lokalen localStorage-Wahl. Wird sowohl vom
  // Passwort-Login (setAuth) als auch vom SSO-Login (loginWithToken)
  // aufgerufen, damit beide Login-Wege konsistent synchronisieren.
  const applyPreferredLanguage = (preferredLanguage) => {
    if (preferredLanguage && SUPPORTED_LOCALES.includes(preferredLanguage)) {
      const apply = () => {
        i18n.global.locale.value = preferredLanguage
        localStorage.setItem(LANGUAGE_STORAGE_KEY, preferredLanguage)
      }
      // Nachgeladene Sprachen erst nach dem Laden ihrer Texte aktivieren.
      if (isLocaleLoaded(preferredLanguage)) apply()
      else loadLocaleMessages(preferredLanguage).then((ok) => ok && apply())
    }
  }

  const setAuth = (newToken, userProfile) => {
    token.value = newToken
    localStorage.setItem('jwt_token', newToken)
    currentUser.value = userProfile

    applyPreferredLanguage(userProfile?.preferred_language)
  }

  // SSO: JWT direkt aus URL-Token dekodieren und setzen
  const loginWithToken = (jwtToken) => {
    try {
      const parts = jwtToken.split('.')
      if (parts.length !== 3) {
        throw new Error('Ungültiges Token-Format')
      }
      const base64Url = parts[1]
      const base64 = base64Url.replaceAll('-', '+').replaceAll('_', '/')
      const jsonPayload = decodeURIComponent(
        atob(base64)
          .split('')
          .map(c => '%' + ('00' + c.codePointAt(0).toString(16)).slice(-2))
          .join('')
      )
      const payload = JSON.parse(jsonPayload)
      token.value = jwtToken
      localStorage.setItem('jwt_token', jwtToken)
      currentUser.value = {
        // id wie bei /api/me, sonst erkennt die
        // Benutzerverwaltung die eigene Zeile nicht.
        id: payload.user_id,
        email: payload.email,
        role: payload.role,
        permissions: payload.permissions,
        is_global_admin: payload.is_global_admin,
        sso_provider: payload.sso,
        name: payload.name,
        // SSO lädt /api/me nicht, die Tooltip-Einstellung kommt
        // deshalb aus dem Token (Claim fehlt = Default an).
        help_tooltips: !payload.hide_help_tooltips,
      }

      applyPreferredLanguage(payload.preferred_language)
    } catch (e) {
      console.error('Ungültiger SSO-Token:', e)
    }
  }

  const logout = () => {
    token.value = ''
    localStorage.removeItem('jwt_token')
    currentUser.value = null
    // Seitenrollen und aktiver Tenant gehören dem abgemeldeten
    // Nutzer; der nächste Nutzer darf sie nicht erben.
    setMyPages([])
    setActiveTenant(null)
    // ein gemerkter Einladungs-Token gehört dem abgemeldeten Nutzer.
    clearInvitationToken()
  }

  const isLoggedIn = computed(() => !!token.value && !!currentUser.value)
  const isGlobalAdmin = computed(() => !!currentUser.value?.is_global_admin)

  return {
    token,
    currentUser,
    hasPerm,
    setAuth,
    loginWithToken,
    logout,
    isLoggedIn,
    isGlobalAdmin
  }
}
