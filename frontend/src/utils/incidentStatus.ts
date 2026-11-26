// einzige Stelle im Frontend, die festlegt, welche Vorfall-Status
// als „behoben“ gelten. Alles andere ist offen und gehört auf der öffentlichen
// Statusseite unter „Aktuelle Vorfälle“:
//
//   offen:   investigating, identified, monitoring (Vorfälle),
//            in_progress (laufende Wartung) und unbekannte Rohwerte
//   behoben: resolved (Vorfälle und Wartungen); der Altwert completed ist per Migration
//            auf resolved umgestellt
//
// Geplante, noch nicht gestartete Wartungen (scheduled) sind weder aktuell
// noch behoben, sie stehen in einer eigenen Sektion (isUpcomingMaintenance).
// Unbekannte Status zählen bewusst als offen: Ein Vorfall verschwindet nur
// dann aus „Aktuelle Vorfälle“, wenn er nachweislich behoben ist.
export const RESOLVED_INCIDENT_STATUSES = Object.freeze(['resolved'])

export const isResolvedStatus = (status) => RESOLVED_INCIDENT_STATUSES.includes(status)

export const isUpcomingMaintenance = (incident) => incident.IsMaintenance && incident.Status === 'scheduled'

// 4a: Vorfall-Status in der Darstellung der Statusseite. Die
// Stufe benennt Form/Farbe von StatusBadge (utils/status.js), der Schlüssel den
// Text. Untersuchung ist die schwerste Stufe (Großstörung), Beobachtung die
// leichteste offene (beeinträchtigt); Wartung bekommt die Wartungsform.
// Unbekannte Rohwerte bleiben neutral (Stufe null) und zeigen den Rohwert.
const INCIDENT_STATUS_META = Object.freeze({
  investigating: { level: 'major', labelKey: 'incidentDetail.statusInvestigating' },
  identified: { level: 'partial', labelKey: 'incidentDetail.statusIdentified' },
  monitoring: { level: 'degraded', labelKey: 'incidentDetail.statusMonitoring' },
  resolved: { level: 'operational', labelKey: 'incidentDetail.statusResolved' },
  scheduled: { level: 'maintenance', labelKey: 'statusPage.statusScheduled' },
  in_progress: { level: 'maintenance', labelKey: 'statusPage.statusInProgress' },
})

export const incidentStatusMeta = (status) =>
  (Object.hasOwn(INCIDENT_STATUS_META, status) ? INCIDENT_STATUS_META[status] : { level: null, labelKey: null })
