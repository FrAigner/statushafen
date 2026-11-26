// Part D: helpers for the public display of external sources.
import type { ExternalStatusRef } from '../types/api'

interface EffectiveStatusNode {
  Status: string
  effective_status?: string
  Children?: EffectiveStatusNode[]
}

// The dashboard delivers per component `Status` (own status + own open
// incidents) and `effective_status` (additionally the worst of all linked
// external components; stale sources do not count). The page shows the
// effective status everywhere. The backend does the computing (EffectiveStatuses);
// the frontend only substitutes the value, so that overall status, filters, badges and
// counters see the same thing without logic of their own. Groups keep "group".
export const applyEffectiveStatus = <T extends EffectiveStatusNode>(nodes: T[] | null | undefined): T[] =>
  (nodes || []).map((n): T => {
    const children = n.Children ? applyEffectiveStatus(n.Children as T[]) : n.Children
    const status = n.Status !== 'group' && n.effective_status ? n.effective_status : n.Status
    return { ...n, Status: status, Children: children }
  })

// Only http(s) addresses are linked: the URL comes from a foreign source,
// `javascript:` and the like must never end up in an href. The result is the
// normalized address or null.
export const safeHttpUrl = (value: unknown): string | null => {
  if (typeof value !== 'string' || value.trim() === '') return null
  try {
    const u = new URL(value.trim())
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : null
  } catch {
    return null
  }
}

// Linked sources of a component that are displayed: not vanished ones
// (they do not count in the effective status either).
export const visibleExternalSources = <T extends ExternalStatusRef>(node: { external_sources?: T[] } | null | undefined): T[] =>
  (node?.external_sources || []).filter((r) => !r.vanished)
