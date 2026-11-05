// Redesign Stufe 3: einzige Stelle, die Komponentenstatus auf
// Stufe, Form und Wortlaut abbildet. StatusIcon, StatusBadge und QuickStatus
// lesen nur hier; Views rechnen nie selbst mit Rohwerten.
//
// Vier Stufen für Komponenten (operational, degraded, partial, major). Wartung
// ist im Backend kein Komponentenstatus (Wartungen sind Vorfälle mit
// Kind "maintenance"), die Statusseite leitet sie aus einer laufenden Wartung
// ab. Deshalb steht sie als fünfte Darstellung bereit, gehört aber nicht in
// STATUS_LEVELS und damit nicht in den Schnellstatus.
//
// Rohwerte des Backends: operational, degraded_performance, partial_outage,
// major_outage (models.go). Die Kurzformen werden ebenfalls verstanden, weil
// Views und Tests sie bereits verwenden.

export const STATUS_LEVELS = Object.freeze(['operational', 'degraded', 'partial', 'major'])
export const STATUS_MAINTENANCE = 'maintenance'

const RAW_BY_LEVEL = Object.freeze({
  operational: 'operational',
  degraded: 'degraded_performance',
  partial: 'partial_outage',
  major: 'major_outage',
})

const LEVEL_BY_VALUE = Object.freeze({
  operational: 'operational',
  degraded: 'degraded',
  degraded_performance: 'degraded',
  partial: 'partial',
  partial_outage: 'partial',
  major: 'major',
  major_outage: 'major',
  maintenance: STATUS_MAINTENANCE,
  // Rohwert des Backends (componentstatus.UnderMaintenance), z. B. aus einer
  // laufenden Wartung bei einer gekoppelten externen Quelle.
  under_maintenance: STATUS_MAINTENANCE,
})

// i18n-Schlüssel (status.*); der Wortlaut „Beeinträchtigt“ bleibt.
const LABEL_KEY = Object.freeze({
  operational: 'status.operational',
  degraded: 'status.degradedPerformance',
  partial: 'status.partialOutage',
  major: 'status.majorOutage',
  maintenance: 'status.maintenance',
})

// Form je Stufe: Status ist immer Form + Farbe + Text, nie nur Farbe.
const SHAPE = Object.freeze({
  operational: 'circle-check',
  degraded: 'triangle-exclamation',
  partial: 'diamond-minus',
  major: 'octagon-cross',
  maintenance: 'square-clock',
})

// Stufe zu einem Roh- oder Kurzwert; unbekannte Werte ergeben null.
export function normalizeStatus(value) {
  return Object.hasOwn(LEVEL_BY_VALUE, value) ? LEVEL_BY_VALUE[value] : null
}

// Rohwert des Backends zu einer der vier Stufen (für Schreibzugriffe);
// Wartung und Unbekanntes ergeben null.
export function toRawStatus(value) {
  const level = normalizeStatus(value)
  return level && Object.hasOwn(RAW_BY_LEVEL, level) ? RAW_BY_LEVEL[level] : null
}

// Alles, was die Darstellung braucht. Unbekannte Werte bekommen eine neutrale
// Darstellung (level null, Form "circle"), damit nie eine Statusfarbe
// behauptet wird, die es nicht gibt.
export function statusMeta(value) {
  const level = normalizeStatus(value)
  if (!level) return { level: null, shape: 'circle', labelKey: null }
  return { level, shape: SHAPE[level], labelKey: LABEL_KEY[level] }
}
