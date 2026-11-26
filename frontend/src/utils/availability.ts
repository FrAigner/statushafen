// 4a: Darstellung der 90-Tage-Verfügbarkeit
// (GET /api/public/pages/{slug}/availability, docs/api/openapi.yaml).
// Reine Funktionen, damit die Zuordnung getestet und nicht in Templates
// verteilt ist.

// Tagesstatus des Backends -> Stufe/Text. `no_data` (Tage vor Anlage der Seite)
// ist bewusst neutral grau und behauptet weder Betrieb noch Ausfall; unbekannte
// Werte werden genauso behandelt.
const DAY_META = Object.freeze({
  operational: { level: 'ok', labelKey: 'status.operational' },
  degraded_performance: { level: 'deg', labelKey: 'status.degradedPerformance' },
  // Schweregrad im Verlauf (Backend: impact der externen Meldung bzw. Gewichtung).
  partial_outage: { level: 'part', labelKey: 'status.partialOutage' },
  major_outage: { level: 'major', labelKey: 'status.majorOutage' },
  under_maintenance: { level: 'maint', labelKey: 'status.maintenance' },
})
const NO_DATA = Object.freeze({ level: 'none', labelKey: 'availability.noData' })

export const availabilityDayMeta = (status) =>
  (Object.hasOwn(DAY_META, status) ? DAY_META[status] : NO_DATA)

// Prozentzahl für die Anzeige. null (keine Daten) ergibt null, damit nie „0 %“
// oder „100 %“ erfunden wird. Es wird abgeschnitten statt gerundet: 99,996 %
// darf nicht als „100,00 %“ erscheinen, wenn es Ausfallzeit gab.
// Ein Formatter je Sprache: eine Seite zeichnet 90 Tage je Komponente, ein
// neuer Intl-Formatter je Aufruf kostete in PageLayoutList Sekunden (Review-Befund 4).
const numberFormats = new Map()
const numberFormat = (locale) => {
  if (!numberFormats.has(locale)) {
    numberFormats.set(locale, new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }))
  }
  return numberFormats.get(locale)
}

export const formatAvailability = (value, locale) => {
  if (typeof value !== 'number' || Number.isNaN(value)) return null
  const truncated = Math.floor(value * 100) / 100
  return numberFormat(locale).format(truncated) + ' %'
}

// UTC-Tag („2026-10-29“) als lokalisiertes Datum, ebenfalls mit einem Formatter
// je Sprache. Mittag UTC, damit die Zeitzone des Browsers den Tag nicht verschiebt.
const dayFormats = new Map()
export const formatDay = (isoDate, locale) => {
  if (!dayFormats.has(locale)) {
    dayFormats.set(locale, new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }))
  }
  return dayFormats.get(locale).format(new Date(`${isoDate}T12:00:00Z`))
}

// Prüft die Antwort so weit, dass die Ansicht sie gefahrlos zeichnen kann.
// Alles andere gilt als „keine Verfügbarkeitsdaten“ (Balken entfallen).
export const parseAvailability = (res) => (res && Array.isArray(res.days) && res.days.length > 0 ? res : null)

// Dauer für den Tooltip als Schlüssel + Parameter für vue-i18n: „2 h 5 min“,
// „45 min“, unter einer Minute „< 1 min“. Negative oder ungültige Werte zählen als 0.
export const durationParts = (seconds) => {
  const total = typeof seconds === 'number' && seconds > 0 ? Math.floor(seconds) : 0
  const minutes = Math.floor(total / 60)
  if (minutes < 1) return { key: 'availability.durationLessThanMinute', params: {} }
  if (minutes < 60) return { key: 'availability.durationMinutes', params: { minutes } }
  return { key: 'availability.durationHoursMinutes', params: { hours: Math.floor(minutes / 60), minutes: minutes % 60 } }
}
