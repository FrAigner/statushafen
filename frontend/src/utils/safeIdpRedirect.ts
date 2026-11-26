// Die Authorize-URL für Verknüpfung und Testanmeldung kommt vom Server und
// stammt letztlich aus dem Discovery-Dokument des Anbieters, den ein Seiten-Admin
// einträgt. Ungeprüft in location.assign gegeben, wäre `javascript:` oder `data:`
// ein Skript- bzw. Phishing-Ziel. Dieser Helfer ist die einzige Stelle, die so eine
// Adresse in ein Navigationsziel verwandelt (Allowlist: https; http nur für
// localhost im Entwicklungsmodus, wie der Mock-Anbieter aus docker-compose.yml).

const LOCAL_HOSTS = new Set(['localhost', '127.0.0.1', '[::1]'])

/**
 * @param {unknown} raw Adresse aus der Serverantwort
 * @returns {string | null} die unveränderte Adresse oder null, wenn sie nicht taugt
 */
export function safeIdpRedirect(raw) {
  if (typeof raw !== 'string') return null
  let u
  try {
    u = new URL(raw)
  } catch {
    return null // relativ, leer oder kaputt
  }
  if (u.protocol === 'https:') return raw
  if (u.protocol === 'http:' && import.meta.env.DEV && LOCAL_HOSTS.has(u.hostname)) return raw
  return null
}
