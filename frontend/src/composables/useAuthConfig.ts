import { ref } from 'vue'
import { useApi } from './useApi'

/** A global OIDC provider offered on the login page. */
export interface AuthProvider {
  id: string
  name: string
}

export interface AuthConfig {
  passwordLogin: boolean
  providers: AuthProvider[]
}

/** Raw response of GET /api/public/auth-config (checked by normalize). */
interface AuthConfigResponse {
  password_login?: unknown
  providers?: unknown
}

// Public auth configuration (GET /api/public/auth-config): whether email/password
// login is enabled and which global OIDC providers are active. Module-scoped state
// like useSystemSettings, so the login page and the profile share one answer. It is
// loaded once; concurrent callers share the request. After an error authConfig stays
// null (no made-up buttons) and the next call tries again.

const authConfig = ref<AuthConfig | null>(null)
const loadFailed = ref(false)
let pending: Promise<void> | null = null

// Only an explicit true enables passwords; providers without id or name are dropped.
const normalize = (data: AuthConfigResponse | null | undefined): AuthConfig => ({
  passwordLogin: data?.password_login === true,
  providers: (Array.isArray(data?.providers) ? data.providers : [])
    .filter((p): p is AuthProvider => typeof p?.id === 'string' && p.id !== '' && typeof p.name === 'string' && p.name !== '')
    .map((p) => ({ id: p.id, name: p.name })),
})

export function useAuthConfig() {
  const { apiFetch } = useApi()

  const loadAuthConfig = (): Promise<void> => {
    // Already known (loaded, or seeded in tests): do not ask again.
    if (authConfig.value) return Promise.resolve()
    if (!pending) {
      // auth: false - the endpoint is public; a stale token must not turn it into a session end.
      pending = apiFetch<AuthConfigResponse | null>('/api/public/auth-config', { auth: false })
        .then((data) => {
          authConfig.value = normalize(data)
          loadFailed.value = false
        })
        .catch(() => {
          loadFailed.value = true
          pending = null
        })
    }
    return pending
  }

  return { authConfig, loadFailed, loadAuthConfig }
}

// Tests only: clear the cache.
export function resetAuthConfig() {
  authConfig.value = null
  loadFailed.value = false
  pending = null
}
