// Redesign stage 4b: map incident, maintenance and notice status to the
// levels of StatusBadge. Views do not compute with raw values themselves and
// set no colours any more (previously .badge-* and inline colours).
// Shape + colour come from statusMeta (utils/status.js), the text from i18n.
// Unknown values show the raw value neutrally, never a status colour.
import type { TranslateFn } from '../types/i18n'
import type { StatusLevel } from './status'

interface BadgeDef {
  status: StatusLevel | null
  labelKey: string
}

export interface Badge {
  status: StatusLevel | null
  label: string
}

const INCIDENT: Readonly<Record<string, BadgeDef>> = Object.freeze({
  investigating: { status: 'major', labelKey: 'incidentDetail.statusInvestigating' },
  identified: { status: 'partial', labelKey: 'incidentDetail.statusIdentified' },
  monitoring: { status: 'degraded', labelKey: 'incidentDetail.statusMonitoring' },
  resolved: { status: 'operational', labelKey: 'incidentDetail.statusResolved' },
})

const MAINTENANCE: Readonly<Record<string, BadgeDef>> = Object.freeze({
  scheduled: { status: 'maintenance', labelKey: 'maintenance.statusScheduled' },
  in_progress: { status: 'degraded', labelKey: 'maintenance.statusInProgress' },
  resolved: { status: 'operational', labelKey: 'maintenance.statusResolved' },
})

const NOTICE: Readonly<Record<string, BadgeDef>> = Object.freeze({
  info: { status: 'maintenance', labelKey: 'notices.levelInfo' },
  warning: { status: 'degraded', labelKey: 'notices.levelWarning' },
  critical: { status: 'major', labelKey: 'notices.levelCritical' },
})

// Post-mortem follow-up actions; "open" stays neutral (no status tone).
const ACTION: Readonly<Record<string, BadgeDef>> = Object.freeze({
  done: { status: 'operational', labelKey: 'incidentDetail.actionDone' },
  in_progress: { status: 'degraded', labelKey: 'incidentDetail.actionInProgress' },
  open: { status: null, labelKey: 'incidentDetail.actionOpen' },
})

function lookup(map: Readonly<Record<string, BadgeDef>>, value: string | null | undefined, t: TranslateFn): Badge {
  if (!Object.hasOwn(map, value as PropertyKey)) return { status: null, label: value ?? '' }
  const { status, labelKey } = map[value as string] as BadgeDef
  return { status, label: t(labelKey) }
}

export const incidentBadge = (value: string | null | undefined, t: TranslateFn): Badge => lookup(INCIDENT, value, t)
export const maintenanceBadge = (value: string | null | undefined, t: TranslateFn): Badge => lookup(MAINTENANCE, value, t)
export const noticeBadge = (value: string | null | undefined, t: TranslateFn): Badge => lookup(NOTICE, value, t)
export const actionBadge = (value: string | null | undefined, t: TranslateFn): Badge => lookup(ACTION, value, t)
