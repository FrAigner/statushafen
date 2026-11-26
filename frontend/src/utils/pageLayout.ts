// reine Hilfsfunktionen für die öffentliche Statusseite. Alle
// Tagesgrenzen in UTC, damit die Ausgabe unabhängig von der Zeitzone des
// Browsers deterministisch bleibt.
// 4a: Die Balkenreihen (buildDayBars, aus Vorfällen geschätzt)
// sind entfallen; die 90 Tage kommen jetzt vom Backend
// (GET /api/public/pages/{slug}/availability, siehe utils/availability.js).

export const normalizePageLayout = (value) => (value === 'liste' ? 'liste' : 'standard')

const SEVERITY = ['operational', 'under_maintenance', 'degraded_performance', 'partial_outage', 'major_outage']

// Schlechtester Status über den gesamten Komponentenbaum.
export const overallStatus = (nodes) => {
  let worst = 0
  const walk = (list) => {
    for (const n of list || []) {
      worst = Math.max(worst, SEVERITY.indexOf(n.Status))
      walk(n.Children)
    }
  }
  walk(nodes)
  return SEVERITY[worst]
}

// 4a: Gesamtstatus oben auf der Seite. Wie overallStatus, aber
// eine laufende Wartung (Vorfall mit IsMaintenance und Status in_progress) macht
// aus „alles in Ordnung“ den Zustand „maintenance“. Eine Störung hat immer
// Vorrang vor einer Wartung.
export const pageOverallStatus = (nodes, incidents) => {
  const worst = overallStatus(nodes)
  const maintenanceRunning = (incidents || []).some(i => i.IsMaintenance && i.Status === 'in_progress')
  // Eine Komponente in Wartung (z. B. wegen einer externen Quelle)
  // zählt wie eine laufende Wartung; jede Störung hat Vorrang.
  if (worst === 'under_maintenance') return 'maintenance'
  return worst === 'operational' && maintenanceRunning ? 'maintenance' : worst
}

// Text des Gesamtstatus je Wert von pageOverallStatus (i18n-Schlüssel).
export const OVERALL_TEXT_KEYS = Object.freeze({
  operational: 'pageLayout.overallOperational',
  degraded_performance: 'pageLayout.overallDegraded',
  partial_outage: 'pageLayout.overallPartial',
  major_outage: 'pageLayout.overallMajor',
  maintenance: 'pageLayout.overallMaintenance',
})

// Blätter des Komponentenbaums (Gruppen zählen nicht), für „x von y laufen normal“.
export const countLeaves = (nodes) => {
  let total = 0
  let operational = 0
  const walk = (list) => {
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

// Ereigniszeit: Wartungen zählen ab ScheduledStartAt, Vorfälle ab
// StartedAt; ohne diese Angabe gilt die Erstellzeit.
export const eventTime = (i) => (i.IsMaintenance ? i.ScheduledStartAt : i.StartedAt) || i.CreatedAt

const dayKey = (d) => new Date(d).toISOString().slice(0, 10)

// Zeitleiste: Tagesgruppen, jüngster Tag und jüngster Vorfall zuerst.
export const groupIncidentsByDay = (incidents) => {
  const sorted = [...incidents].sort((a, b) => new Date(eventTime(b)) - new Date(eventTime(a)))
  const groups = []
  for (const i of sorted) {
    const date = dayKey(eventTime(i))
    const last = groups[groups.length - 1]
    if (last && last.date === date) last.items.push(i)
    else groups.push({ date, items: [i] })
  }
  return groups
}
