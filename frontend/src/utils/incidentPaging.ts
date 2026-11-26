// Vorfall-Listen kommen seitenweise vom Server (Standard 50, höchstens 200
// je Aufruf). Die öffentliche Statusseite bekommt im Dashboard alle offenen und
// die neuesten behobenen Vorfälle und lädt ältere über die Liste nach; die
// Admin-Listen holen 200 auf einmal und blättern dann weiter.

export const PUBLIC_PAGE_SIZE = 50
export const ADMIN_PAGE_SIZE = 200

/**
 * URL der Vorfall-Liste einer Seite.
 * @param {number|string} pageId
 * @param {{maintenance?: boolean|'any', state?: 'open'|'resolved', limit?: number, offset?: number}} [opts]
 */
export function incidentListUrl(pageId, { maintenance, state, limit, offset } = {}) {
  const q = new URLSearchParams()
  if (maintenance !== undefined) q.set('is_maintenance', String(maintenance))
  if (state) q.set('state', state)
  if (limit) q.set('limit', String(limit))
  if (offset) q.set('offset', String(offset))
  const query = q.toString()
  return `/api/pages/${pageId}/incidents${query ? `?${query}` : ''}`
}

/**
 * Hängt weitere Vorfälle an eine Liste an; schon vorhandene IDs bleiben, wie sie
 * sind (die Dashboard-Daten werden regelmäßig neu geladen und überlappen sich
 * mit nachgeladenen Seiten).
 */
export function mergeIncidents(base, more) {
  const seen = new Set(base.map(i => i.ID))
  const added = more.filter(i => !seen.has(i.ID))
  return added.length === 0 ? base : [...base, ...added]
}
