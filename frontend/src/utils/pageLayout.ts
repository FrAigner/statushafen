// Pure helper functions for the public status page. All day boundaries
// in UTC, so that the output stays deterministic regardless of the browser's
// time zone.
// 4a: the bar rows (buildDayBars, estimated from incidents) are gone;
// the 90 days now come from the backend
// (GET /api/public/pages/{slug}/availability, see utils/availability.ts).

interface StatusNode {
  Status: string
  Children?: StatusNode[] | null
}

interface TimedIncident {
  IsMaintenance?: boolean
  Status?: string
  ScheduledStartAt?: string | null
  StartedAt?: string | null
  CreatedAt?: string
}

export interface IncidentDayGroup<T> {
  date: string
  items: T[]
}

export const normalizePageLayout = (value: unknown): 'liste' | 'standard' => (value === 'liste' ? 'liste' : 'standard')

const SEVERITY = ['operational', 'under_maintenance', 'degraded_performance', 'partial_outage', 'major_outage']

// Worst status across the entire component tree.
export const overallStatus = (nodes: readonly StatusNode[] | null | undefined): string => {
  let worst = 0
  const walk = (list: readonly StatusNode[] | null | undefined) => {
    for (const n of list || []) {
      worst = Math.max(worst, SEVERITY.indexOf(n.Status))
      walk(n.Children)
    }
  }
  walk(nodes)
  return SEVERITY[worst] as string
}

// 4a: overall status at the top of the page. Like overallStatus, but
// a running maintenance (incident with IsMaintenance and status in_progress) turns
// "all good" into the state "maintenance". An outage always takes
// precedence over a maintenance.
export const pageOverallStatus = (nodes: readonly StatusNode[] | null | undefined, incidents: readonly TimedIncident[] | null | undefined): string => {
  const worst = overallStatus(nodes)
  const maintenanceRunning = (incidents || []).some(i => i.IsMaintenance && i.Status === 'in_progress')
  // A component under maintenance (e.g. because of an external source)
  // counts like a running maintenance; any outage takes precedence.
  if (worst === 'under_maintenance') return 'maintenance'
  return worst === 'operational' && maintenanceRunning ? 'maintenance' : worst
}

// Text of the overall status per value of pageOverallStatus (i18n key).
export const OVERALL_TEXT_KEYS: Readonly<Record<string, string>> = Object.freeze({
  operational: 'pageLayout.overallOperational',
  degraded_performance: 'pageLayout.overallDegraded',
  partial_outage: 'pageLayout.overallPartial',
  major_outage: 'pageLayout.overallMajor',
  maintenance: 'pageLayout.overallMaintenance',
})

// Leaves of the component tree (groups do not count), for "x of y operating normally".
export const countLeaves = (nodes: readonly StatusNode[] | null | undefined): { total: number; operational: number } => {
  let total = 0
  let operational = 0
  const walk = (list: readonly StatusNode[] | null | undefined) => {
    for (const n of list || []) {
      if (n.Children && n.Children.length > 0) {
        walk(n.Children)
      } else {
        total += 1
        if (n.Status === 'operational') operational += 1
      }
    }
  }
  walk(nodes)
  return { total, operational }
}

// Event time: maintenances count from ScheduledStartAt, incidents from
// StartedAt; without it, the creation time applies.
export const eventTime = (i: TimedIncident): string | null | undefined => (i.IsMaintenance ? i.ScheduledStartAt : i.StartedAt) || i.CreatedAt

const dayKey = (d: string | null | undefined) => new Date(d as string).toISOString().slice(0, 10)

// Timeline: day groups, newest day and newest incident first.
export const groupIncidentsByDay = <T extends TimedIncident>(incidents: readonly T[]): IncidentDayGroup<T>[] => {
  const sorted = [...incidents].sort((a, b) => +new Date(eventTime(b) as string) - +new Date(eventTime(a) as string))
  const groups: IncidentDayGroup<T>[] = []
  for (const i of sorted) {
    const date = dayKey(eventTime(i))
    const last = groups[groups.length - 1]
    if (last && last.date === date) last.items.push(i)
    else groups.push({ date, items: [i] })
  }
  return groups
}
