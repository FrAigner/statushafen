import { useAuth } from './useAuth'
import i18n from '../i18n'
import type { ApiError, ApiErrorBody } from '../types/api'

/** Options of apiFetch: fetch's RequestInit plus the app's own switches. */
export interface ApiFetchOptions extends Omit<RequestInit, 'headers'> {
  /** false sends no token (login, public endpoints). */
  auth?: boolean
  /** HTTP statuses that are normal for this call: the error is still thrown, but not logged. */
  expectedStatuses?: number[]
  headers?: Record<string, string>
}

/**
 * apiFetch from useApi. T is the shape of the JSON response; it is not checked at runtime.
 * A 204 No Content resolves with null: callers of such endpoints pass T = null (or ignore the result).
 */
export type ApiFetch = <T = unknown>(url: string, options?: ApiFetchOptions) => Promise<T>

const asApiError = (err: unknown): ApiError | undefined => err as ApiError | undefined

// 403 is a distinct, recognizable error case, so admin pages can show the
// permission message instead of a generic loading error.
export const isForbidden = (err: unknown): err is ApiError => asApiError(err)?.forbidden === true

// Only a 401 on a request that sent an app token ends the session. Callers
// recognize the case by the flag rather than by the (now translated) message.
export const isSessionExpired = (err: unknown): err is ApiError => asApiError(err)?.sessionExpired === true

// Prod/staging sit behind Caddy's HTTP Basic Auth. Its 401 announces itself
// with `WWW-Authenticate: Basic` and concerns the upstream access, not the app
// token, so it must never log the user out. The backend itself never sets this
// header.
export const isBasicAuthChallenge = (res: Pick<Response, 'headers'> | null | undefined): boolean =>
  /^basic\b/i.test(res?.headers?.get?.('WWW-Authenticate') || '')

const tr = (key: string, params?: Record<string, unknown>): string => i18n.global.t(key, params ?? {})

// Besides `error` (fallback text, partly English or technical) the backend
// sends a stable `code`. If there is a translation under api.errors.<code>, it
// wins; otherwise the server text stays.
const errorMessage = (data: ApiErrorBody, status: number): string => {
  const key = typeof data?.code === 'string' ? `api.errors.${data.code}` : ''
  // retry_after (seconds) is available to messages such as webhook_rate_limited as {seconds}.
  // count/pages for language_is_default and language_in_use.
  if (key && i18n.global.te(key)) return tr(key, { seconds: data?.retry_after, count: data?.count, pages: data?.pages?.join(', ') })
  return data?.error || tr('api.httpError', { status })
}

// If fetch itself fails (server unreachable, offline, CORS), the browser
// throws an English TypeError that differs per browser ("Failed to fetch",
// "Load failed", "NetworkError ..."). The user gets a translated message
// instead; the details stay available as `cause` for the console.
export async function safeFetch(url: string, options?: RequestInit): Promise<Response> {
  try {
    return await fetch(url, options)
  } catch (cause) {
    throw Object.assign(new Error(tr('api.networkError')), { network: true, cause })
  }
}

// A success response with an unreadable body (e.g. a proxy's HTML page)
// used to show the raw JSON parser error ("Unexpected token '<' ...").
export async function readJson<T = unknown>(res: Response): Promise<T> {
  try {
    return await res.json()
  } catch (cause) {
    throw Object.assign(new Error(tr('api.errors.server_error')), { status: res.status, cause })
  }
}

export function useApi() {
  const { token, logout } = useAuth()

  // auth: false sends no token (login). A stale token still stored would
  // otherwise turn a failed login attempt into the end of a session.
  // expectedStatuses: HTTP statuses that are normal for this call
  // (e.g. 404 = "no subscription"). The error is still thrown, but not
  // logged as an error via console.error.
  const apiFetch: ApiFetch = async <T = unknown>(url: string, { auth = true, expectedStatuses = [], ...options }: ApiFetchOptions = {}): Promise<T> => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json', ...options.headers }
    // A custom header instead of Authorization, which Caddy's Basic Auth
    // occupies. If the app put a bearer token there, the browser stopped
    // sending the Basic credentials and Caddy answered 401.
    const sentToken = auth && !!token.value
    if (sentToken) headers['X-Auth-Token'] = token.value

    let fetchUrl = url
    if (!url.startsWith('http')) {
      const apiBase = import.meta.env.VITE_API_URL || ''
      fetchUrl = `${apiBase}${url}`
    }

    try {
      const res = await safeFetch(fetchUrl, { ...options, headers })

      if (res.status === 401 && isBasicAuthChallenge(res)) {
        throw Object.assign(new Error(tr('api.basicAuthRequired')), { status: 401, basicAuth: true })
      }
      // Without a sent token (failed login attempt, anonymous request for a
      // private page) a 401 is an ordinary error with the server's message.
      if (res.status === 401 && sentToken) {
        logout()
        throw Object.assign(new Error(tr('api.sessionExpired')), { status: 401, sessionExpired: true })
      }
      if (res.status === 403) {
        // Read the body too, so callers can evaluate the error code (e.g.
        // confirmation_invalid); the fixed message stays.
        const data: ApiErrorBody = await res.json().catch(() => ({}))
        throw Object.assign(new Error(tr('api.forbidden')), { status: 403, forbidden: true, data })
      }
      if (!res.ok) {
        const data: ApiErrorBody = await res.json().catch(() => ({}))
        // status on the error, so callers can tell e.g. a 404 (normal case) from real errors;
        // data carries the body along (page list of the 409 on DELETE /api/me)
        throw Object.assign(new Error(errorMessage(data, res.status)), { status: res.status, data })
      }

      // Some DELETE requests might return 204 No Content
      // The caller's T covers this case (see ApiFetch); null is not a T in general.
      if (res.status === 204) return null as T

      return await readJson<T>(res)
    } catch (err) {
      const status = asApiError(err)?.status
      if (!isSessionExpired(err) && !(status !== undefined && expectedStatuses.includes(status))) {
        console.error("apiFetch error:", err)
      }
      throw err;
    }
  }

  return { apiFetch }
}
