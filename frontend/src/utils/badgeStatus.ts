// Redesign Stufe 4b: Vorfall-, Wartungs- und Ankündigungsstatus
// auf die Stufen von StatusBadge abbilden. Die Views rechnen nicht selbst mit
// Rohwerten und setzen keine Farben mehr (vorher .badge-* und Inline-Farben).
// Form + Farbe kommen aus statusMeta (utils/status.js), der Text aus i18n.
// Unbekannte Werte zeigen neutral den Rohwert, nie eine Statusfarbe.

const INCIDENT = Object.freeze({
  investigating: { status: 'major', labelKey: 'incidentDetail.statusInvestigating' },
  identified: { status: 'partial', labelKey: 'incidentDetail.statusIdentified' },
  monitoring: { status: 'degraded', labelKey: 'incidentDetail.statusMonitoring' },
  resolved: { status: 'operational', labelKey: 'incidentDetail.statusResolved' },
})

const MAINTENANCE = Object.freeze({
  scheduled: { status: 'maintenance', labelKey: 'maintenance.statusScheduled' },
  in_progress: { status: 'degraded', labelKey: 'maintenance.statusInProgress' },
  resolved: { status: 'operational', labelKey: 'maintenance.statusResolved' },
})

const NOTICE = Object.freeze({
  info: { status: 'maintenance', labelKey: 'notices.levelInfo' },
  warning: { status: 'degraded', labelKey: 'notices.levelWarning' },
  critical: { status: 'major', labelKey: 'notices.levelCritical' },
})

// Folgemaßnahmen des Post-Mortems; "offen" bleibt neutral (kein Statuston).
const ACTION = Object.freeze({
  done: { status: 'operational', labelKey: 'incidentDetail.actionDone' },
  in_progress: { status: 'degraded', labelKey: 'incidentDetail.actionInProgress' },
  open: { status: null, labelKey: 'incidentDetail.actionOpen' },
})

function lookup(map, value, t) {
  if (!Object.hasOwn(map, value)) return { status: null, label: value ?? '' }
  const { status, labelKey } = map[value]
  return { status, label: t(labelKey) }
}

export const incidentBadge = (value, t) => lookup(INCIDENT, value, t)
export const maintenanceBadge = (value, t) => lookup(MAINTENANCE, value, t)
export const noticeBadge = (value, t) => lookup(NOTICE, value, t)
export const actionBadge = (value, t) => lookup(ACTION, value, t)
