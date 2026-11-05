import { useAuth } from './useAuth'
import i18n from '../i18n'

// 403 ist ein eigener, erkennbarer Fehlerfall, damit Admin-Seiten
// statt eines generischen Ladefehlers die Berechtigungsmeldung zeigen können.
export const isForbidden = (err) => err?.forbidden === true

// Nur eine 401 auf eine Anfrage, die ein App-Token mitgeschickt hat,
// beendet die Sitzung. Aufrufer erkennen den Fall am Flag statt am (jetzt
// übersetzten) Meldungstext.
export const isSessionExpired = (err) => err?.sessionExpired === true

// Vor Prod/Staging liegt Caddys HTTP Basic Auth. Deren 401
// kündigt sich mit `WWW-Authenticate: Basic` an und betrifft den vorgeschalteten
// Zugang, nicht das App-Token – darf also nie zum Ausloggen führen. Das Backend
// selbst setzt diesen Header nicht.
export const isBasicAuthChallenge = (res) =>
  /^basic\b/i.test(res?.headers?.get?.('WWW-Authenticate') || '')

const tr = (key, params) => i18n.global.t(key, params)

// Das Backend schickt neben `error` (Fallbacktext, teils englisch
// oder technisch) einen stabilen `code`. Gibt es dafür eine Übersetzung unter
// api.errors.<code>, gewinnt sie; sonst bleibt der Servertext.
const errorMessage = (data, status) => {
  const key = typeof data?.code === 'string' ? `api.errors.${data.code}` : ''
  // retry_after (Sekunden) steht Meldungen wie webhook_rate_limited als {seconds} zur Verfügung.
  // count/pages für language_is_default und language_in_use.
  if (key && i18n.global.te(key)) return tr(key, { seconds: data?.retry_after, count: data?.count, pages: data?.pages?.join(', ') })
  return data?.error || tr('api.httpError', { status })
}

// Schlägt fetch selbst fehl (Server nicht erreichbar, Offline, CORS),
// wirft der Browser einen englischen, je Browser anderen TypeError ("Failed to
// fetch", "Load failed", "NetworkError ..."). Der Nutzer bekommt stattdessen eine
// übersetzte Meldung; Details bleiben als `cause` für die Konsole erhalten.
export async function safeFetch(url, options) {
  try {
    return await fetch(url, options)
  } catch (cause) {
    throw Object.assign(new Error(tr('api.networkError')), { network: true, cause })
  }
}

// Eine Erfolgsantwort mit unlesbarem Body (z. B. HTML-Seite eines
// Proxys) zeigte den rohen JSON-Parserfehler ("Unexpected token '<' ...").
export async function readJson(res) {
  try {
    return await res.json()
  } catch (cause) {
    throw Object.assign(new Error(tr('api.errors.server_error')), { status: res.status, cause })
  }
}

export function useApi() {
  const { token, logout } = useAuth()

  // auth: false schickt kein Token mit (Login). Ein dabei noch gespeichertes,
  // veraltetes Token würde sonst einen Login-Fehlversuch zum Sitzungsende
  // machen.
  // expectedStatuses: HTTP-Status, die für diesen Aufruf ein Normalfall sind
  // (z. B. 404 = „kein Abonnement“). Der Fehler wird weiter
  // geworfen, aber nicht per console.error als Fehler protokolliert.
  const apiFetch = async (url, { auth = true, expectedStatuses = [], ...options } = {}) => {
    const headers = { 'Content-Type': 'application/json', ...options.headers }
    // Eigener Header statt Authorization, den belegt Caddys Basic
    // Auth. Setzte die App dort ein Bearer-Token, schickte der Browser die
    // Basic-Zugangsdaten nicht mehr mit und Caddy antwortete 401.
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
      // Ohne gesendetes Token (Login-Fehlversuch, anonymer Aufruf
      // einer privaten Seite) ist eine 401 ein normaler Fehler mit Server-Meldung.
      if (res.status === 401 && sentToken) {
        logout()
        throw Object.assign(new Error(tr('api.sessionExpired')), { status: 401, sessionExpired: true })
      }
      if (res.status === 403) {
        // Body mitlesen, damit Aufrufer den Fehlercode (z. B.
        // confirmation_invalid) auswerten können; die feste Meldung bleibt.
        const data = await res.json().catch(() => ({}))
        throw Object.assign(new Error(tr('api.forbidden')), { status: 403, forbidden: true, data })
      }
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        // status am Fehler, damit Aufrufer z. B. 404 (Normalfall) von echten Fehlern unterscheiden können;
        // data trägt den Body weiter (Seitenliste der 409 bei DELETE /api/me)
        throw Object.assign(new Error(errorMessage(data, res.status)), { status: res.status, data })
      }

      // Some DELETE requests might return 204 No Content
      if (res.status === 204) return null

      return await readJson(res)
    } catch (err) {
      if (!isSessionExpired(err) && !expectedStatuses.includes(err?.status)) {
        console.error("apiFetch error:", err)
      }
      throw err;
    }
  }

  return { apiFetch }
}
