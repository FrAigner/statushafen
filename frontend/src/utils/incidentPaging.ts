// Incident lists come from the server page by page (default 50, at most 200
// per call). The public status page gets all open and the newest resolved
// incidents in the dashboard and loads older ones through the list; the
// admin lists fetch 200 at once and then page further.

export const PUBLIC_PAGE_SIZE = 50
export const ADMIN_PAGE_SIZE = 200

export interface IncidentListOptions {
  maintenance?: boolean | 'any'
  state?: 'open' | 'resolved'
  limit?: number
  offset?: number
}

/** URL of a page's incident list. */
export function incidentListUrl(pageId: number | string, { maintenance, state, limit, offset }: IncidentListOptions = {}): string {
  const q = new URLSearchParams()
  if (maintenance !== undefined) q.set('is_maintenance', String(maintenance))
  if (state) q.set('state', state)
  if (limit) q.set('limit', String(limit))
  if (offset) q.set('offset', String(offset))
  const query = q.toString()
  return `/api/pages/${pageId}/incidents${query ? `?${query}` : ''}`
}

/**
 * Appends further incidents to a list; IDs already present stay as they
 * are (the dashboard data is reloaded regularly and overlaps with pages
 * loaded later).
 */
export function mergeIncidents<T extends { ID: number }>(base: T[], more: T[]): T[] {
  const seen = new Set(base.map(i => i.ID))
  const added = more.filter(i => !seen.has(i.ID))
  return added.length === 0 ? base : [...base, ...added]
}
