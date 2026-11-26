import { ref, computed } from 'vue'
import { useTenant } from './useTenant'
import i18n, { SUPPORTED_LOCALES, LANGUAGE_STORAGE_KEY, isLocaleLoaded, loadLocaleMessages } from '../i18n'
import { clearInvitationToken } from '../utils/invitationToken'
import { grants, isGlobalResource } from '../utils/permissions'
import type { AuthUser } from '../types/api'

const token = ref(localStorage.getItem('jwt_token') || '')
const currentUser = ref<AuthUser | null>(null)

/** Claims of the JWT that the SSO login receives in the URL. */
interface SsoTokenPayload {
  user_id?: number
  email?: string
  role?: string
  permissions?: string
  is_global_admin?: boolean
  sso?: string
  name?: string
  hide_help_tooltips?: boolean
  preferred_language?: string
}

export function useAuth() {
  const { activeTenantPermissions, setMyPages, setActiveTenant } = useTenant()

  // Like the backend (middleware.requirePermissionCheck), only the role on
  // the currently active status page (UserStatusPageRole) counts; the global
  // role in the JWT (currentUser.permissions) is never evaluated, otherwise
  // the UI shows write buttons that end in a 403. Global admins always
  // bypass. create, update and delete of a resource imply its read; the
  // legacy value `:write` counts as all four actions (backend:
  // permissions.Has). Global resources (users, roles, tags, ...) belong to
  // global admins only, even if a page role carries the string.
  const hasPerm = (required: string): boolean => {
    if (currentUser.value?.is_global_admin) return true
    if (!currentUser.value) return false
    if (isGlobalResource(required.split(':')[0] as string)) return false
    return grants(activeTenantPermissions.value, required)
  }

  // Applies a language preference stored on the server to the active UI
  // language, so it wins across devices over a possibly different local
  // localStorage choice. Called by both the password login (setAuth) and the
  // SSO login (loginWithToken), so both login paths sync consistently.
  const applyPreferredLanguage = (preferredLanguage: string | undefined) => {
    if (preferredLanguage && SUPPORTED_LOCALES.includes(preferredLanguage)) {
      const apply = () => {
        // vue-i18n types the locale as one of the bundled message locales; lazily loaded ones are valid too.
        i18n.global.locale.value = preferredLanguage as typeof i18n.global.locale.value
        localStorage.setItem(LANGUAGE_STORAGE_KEY, preferredLanguage)
      }
      // Activate lazily loaded languages only once their texts are loaded.
      if (isLocaleLoaded(preferredLanguage)) apply()
      else loadLocaleMessages(preferredLanguage).then((ok) => ok && apply())
    }
  }

  const setAuth = (newToken: string, userProfile: AuthUser | null) => {
    token.value = newToken
    localStorage.setItem('jwt_token', newToken)
    currentUser.value = userProfile

    applyPreferredLanguage(userProfile?.preferred_language)
  }

  // SSO: decode the JWT directly from the URL token and set it
  const loginWithToken = (jwtToken: string) => {
    try {
      const parts = jwtToken.split('.')
      if (parts.length !== 3) {
        throw new Error('Ungültiges Token-Format')
      }
      const base64Url = parts[1]!
      const base64 = base64Url.replaceAll('-', '+').replaceAll('_', '/')
      const jsonPayload = decodeURIComponent(
        atob(base64)
          .split('')
          .map(c => '%' + ('00' + c.codePointAt(0)!.toString(16)).slice(-2))
          .join('')
      )
      const payload = JSON.parse(jsonPayload) as SsoTokenPayload
      token.value = jwtToken
      localStorage.setItem('jwt_token', jwtToken)
      currentUser.value = {
        // id as with /api/me, otherwise the user management does not
        // recognize the user's own row.
        id: payload.user_id,
        email: payload.email,
        role: payload.role,
        permissions: payload.permissions,
        is_global_admin: payload.is_global_admin,
        sso_provider: payload.sso,
        name: payload.name,
        // SSO does not load /api/me, so the tooltip setting comes from
        // the token (missing claim = default on).
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
    // Page roles and the active tenant belong to the logged-out user;
    // the next user must not inherit them.
    setMyPages([])
    setActiveTenant(null)
    // a remembered invitation token belongs to the logged-out user.
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
