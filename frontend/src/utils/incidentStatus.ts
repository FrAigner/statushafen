// The only place in the frontend that decides which incident statuses count
// as "resolved". Everything else is open and belongs under "Current incidents"
// on the public status page:
//
//   open:     investigating, identified, monitoring (incidents),
//             in_progress (running maintenance) and unknown raw values
//   resolved: resolved (incidents and maintenances); the legacy value completed was
//             migrated to resolved
//
// Scheduled maintenances that have not started yet (scheduled) are neither current
// nor resolved; they live in a section of their own (isUpcomingMaintenance).
// Unknown statuses deliberately count as open: an incident only disappears
// from "Current incidents" once it is demonstrably resolved.
import type { StatusLevel } from './status'

export interface IncidentStatusMeta {
  level: StatusLevel | null
  labelKey: string | null
}

export const RESOLVED_INCIDENT_STATUSES: readonly string[] = Object.freeze(['resolved'])

export const isResolvedStatus = (status: string): boolean => RESOLVED_INCIDENT_STATUSES.includes(status)

export const isUpcomingMaintenance = (incident: { IsMaintenance?: boolean; Status: string }) => incident.IsMaintenance && incident.Status === 'scheduled'

// 4a: incident status as presented on the status page. The
// level names shape/colour of StatusBadge (utils/status.js), the key the
// text. Investigating is the most severe level (major outage), monitoring the
// mildest open one (degraded); maintenance gets the maintenance shape.
// Unknown raw values stay neutral (level null) and show the raw value.
const INCIDENT_STATUS_META: Readonly<Record<string, IncidentStatusMeta>> = Object.freeze({
  investigating: { level: 'major', labelKey: 'incidentDetail.statusInvestigating' },
  identified: { level: 'partial', labelKey: 'incidentDetail.statusIdentified' },
  monitoring: { level: 'degraded', labelKey: 'incidentDetail.statusMonitoring' },
  resolved: { level: 'operational', labelKey: 'incidentDetail.statusResolved' },
  scheduled: { level: 'maintenance', labelKey: 'statusPage.statusScheduled' },
  in_progress: { level: 'maintenance', labelKey: 'statusPage.statusInProgress' },
})

export const incidentStatusMeta = (status: string | null | undefined): IncidentStatusMeta =>
  (Object.hasOwn(INCIDENT_STATUS_META, status as PropertyKey) ? INCIDENT_STATUS_META[status as string] as IncidentStatusMeta : { level: null, labelKey: null })
